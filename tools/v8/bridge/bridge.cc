#include "bridge.h"

#include <algorithm>
#include <atomic>
#include <chrono>
#include <cmath>
#include <cstdlib>
#include <cstring>
#include <limits>
#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>
#include <unordered_set>
#include <utility>
#include <vector>

#include "include/libplatform/libplatform.h"
#include "include/v8-array-buffer.h"
#include "include/v8-context.h"
#include "include/v8-container.h"
#include "include/v8-exception.h"
#include "include/v8-external.h"
#include "include/v8-function.h"
#include "include/v8-initialization.h"
#include "include/v8-isolate.h"
#include "include/v8-locker.h"
#include "include/v8-message.h"
#include "include/v8-microtask.h"
#include "include/v8-object.h"
#include "include/v8-persistent-handle.h"
#include "include/v8-primitive.h"
#include "include/v8-profiler.h"
#include "include/v8-script.h"
#include "include/v8-statistics.h"
#include "include/v8-template.h"

namespace {

constexpr int kNodeDocumentField = 0;
constexpr int kNodeIDField = 1;
constexpr int kNodeStyleField = 2;
constexpr int kNodeChildNodesField = 3;
constexpr int kNodeChildrenField = 4;
constexpr int kNodeClassListField = 5;
constexpr int kNodeDatasetField = 6;
constexpr int kNodeInternalFieldCount = 7;
constexpr int kStyleNodeField = 0;
constexpr int kFacadeNodeField = 0;
constexpr int kFacadeBackingField = 1;
constexpr int kFacadeInternalFieldCount = 2;
constexpr int kIteratorFacadeField = 0;
constexpr int kIteratorIndexField = 1;
constexpr int kIteratorSourceKindField = 2;
constexpr int kIteratorModeField = 3;
constexpr int kEventStateField = 0;
constexpr uint8_t kEventPhaseNone = 0;
constexpr uint8_t kEventPhaseCapturing = 1;
constexpr uint8_t kEventPhaseAtTarget = 2;
constexpr uint8_t kEventPhaseBubbling = 3;
constexpr int64_t kMaximumDelayMilliseconds =
    std::numeric_limits<int64_t>::max() / 1000000;

std::once_flag runtime_once;
std::unique_ptr<v8::Platform> runtime_platform;
bool runtime_ready = false;
std::string runtime_error;

struct WrapperKey {
  uint64_t document = 0;
  uint32_t node = 0;

  bool operator==(const WrapperKey &other) const {
    return document == other.document && node == other.node;
  }
};

struct WrapperKeyHash {
  size_t operator()(const WrapperKey &key) const {
    size_t first = std::hash<uint64_t>{}(key.document);
    size_t second = std::hash<uint32_t>{}(key.node);
    return first ^ (second + 0x9e3779b9U + (first << 6U) + (first >> 2U));
  }
};

struct ListenerKey {
  WrapperKey target;
  std::string type;

  bool operator==(const ListenerKey &other) const {
    return target == other.target && type == other.type;
  }
};

struct ListenerKeyHash {
  size_t operator()(const ListenerKey &key) const {
    size_t target = WrapperKeyHash{}(key.target);
    size_t type = std::hash<std::string>{}(key.type);
    return target ^ (type + 0x9e3779b9U + (target << 6U) + (target >> 2U));
  }
};

struct ListenerRecord {
  uint64_t id = 0;
  v8::Global<v8::Function> callback;
  bool capture = false;
  bool once = false;
  bool passive = false;
  bool removed = false;
};

enum class EventInterface : uint8_t {
  Event,
  MouseEvent,
  PointerEvent,
  KeyboardEvent,
  InputEvent,
  FocusEvent,
};

enum class FacadeKind : int32_t {
  NodeList = 1,
  HTMLCollection = 2,
  ClassList = 3,
};

enum class IteratorMode : int32_t {
  Keys = 1,
  Values = 2,
  Entries = 3,
};

struct EventState {
  EventInterface interface = EventInterface::Event;
  std::string type;
  bool bubbles = false;
  bool cancelable = false;
  bool composed = false;
  bool default_prevented = false;
  bool propagation_stopped = false;
  bool immediate_stopped = false;
  bool dispatching = false;
  bool in_passive_listener = false;
  bool trusted = false;
  uint8_t phase = kEventPhaseNone;
  bool has_target = false;
  bool has_current_target = false;
  bool has_related_target = false;
  WrapperKey target;
  WrapperKey current_target;
  WrapperKey related_target;
  std::vector<WrapperKey> path;
  double timestamp = 0;
  double client_x = 0;
  double client_y = 0;
  int32_t button = 0;
  uint32_t buttons = 0;
  int32_t pointer_id = 0;
  std::string pointer_type;
  bool is_primary = false;
  std::string key;
  std::string code;
  std::string data;
  std::string input_type;
  bool repeat = false;
  bool is_composing = false;
  bool alt_key = false;
  bool ctrl_key = false;
  bool meta_key = false;
  bool shift_key = false;
};

struct WrapperWeakData;
struct EventWeakData;

struct WrapperEntry {
  v8::Global<v8::Object> object;
  WrapperWeakData *weak = nullptr;
};

uint64_t MonotonicNanos() {
  return static_cast<uint64_t>(
      std::chrono::duration_cast<std::chrono::nanoseconds>(
          std::chrono::steady_clock::now().time_since_epoch())
          .count());
}

void SetError(char **error_out, const std::string &message) {
  if (error_out == nullptr)
    return;
  *error_out = static_cast<char *>(std::malloc(message.size() + 1));
  if (*error_out == nullptr)
    return;
  std::memcpy(*error_out, message.data(), message.size());
  (*error_out)[message.size()] = '\0';
}

std::string TakeCString(char *value) {
  if (value == nullptr)
    return {};
  std::string result(value);
  std::free(value);
  return result;
}

std::string UTF8Value(v8::Isolate *isolate, v8::Local<v8::Value> value) {
  if (value.IsEmpty())
    return {};
  v8::String::Utf8Value utf8(isolate, value);
  if (*utf8 == nullptr)
    return {};
  return std::string(*utf8, utf8.length());
}

std::string DescribeException(v8::Isolate *isolate,
                              v8::Local<v8::Context> context,
                              const v8::TryCatch &caught) {
  std::string result = "JavaScript exception";
  std::string exception = UTF8Value(isolate, caught.Exception());
  if (!exception.empty())
    result += ": " + exception;

  v8::Local<v8::Message> message = caught.Message();
  if (!message.IsEmpty()) {
    std::string resource =
        UTF8Value(isolate, message->GetScriptOrigin().ResourceName());
    int line = message->GetLineNumber(context).FromMaybe(0);
    int column = message->GetStartColumn(context).FromMaybe(0) + 1;
    if (!resource.empty()) {
      result += " at " + resource;
      if (line > 0)
        result += ":" + std::to_string(line) + ":" + std::to_string(column);
    }
  }

  v8::Local<v8::Value> stack;
  if (caught.StackTrace(context).ToLocal(&stack)) {
    std::string rendered = UTF8Value(isolate, stack);
    if (!rendered.empty() && rendered != exception)
      result += "\n" + rendered;
  }
  return result;
}

void InitializeRuntime(const char *icu_data_path) {
  bool icu_ready = false;
  if (icu_data_path != nullptr && icu_data_path[0] != '\0') {
    icu_ready = v8::V8::InitializeICUDefaultLocation(nullptr, icu_data_path);
  } else {
    icu_ready = v8::V8::InitializeICUDefaultLocation(nullptr);
  }
  if (!icu_ready) {
    runtime_error = "V8 failed to initialize ICU data";
    return;
  }

  runtime_platform = v8::platform::NewDefaultPlatform();
  if (!runtime_platform) {
    runtime_error = "V8 failed to create its default platform";
    return;
  }
  v8::V8::InitializePlatform(runtime_platform.get());
  if (!v8::V8::Initialize()) {
    runtime_error = "V8 initialization failed";
    return;
  }
  runtime_ready = true;
}

void FillHeapStatistics(v8::Isolate *isolate,
                        gossamer_v8_heap_statistics *output) {
  v8::HeapStatistics statistics;
  isolate->GetHeapStatistics(&statistics);
  output->total_heap_size = statistics.total_heap_size();
  output->total_heap_executable = statistics.total_heap_size_executable();
  output->total_physical_size = statistics.total_physical_size();
  output->total_available_size = statistics.total_available_size();
  output->used_heap_size = statistics.used_heap_size();
  output->heap_size_limit = statistics.heap_size_limit();
  output->malloced_memory = statistics.malloced_memory();
  output->external_memory = statistics.external_memory();
  output->peak_malloced_memory = statistics.peak_malloced_memory();
  output->native_contexts = statistics.number_of_native_contexts();
  output->detached_contexts = statistics.number_of_detached_contexts();
  output->global_handles_size = statistics.total_global_handles_size();
  output->used_global_handles_size = statistics.used_global_handles_size();
  output->total_allocated_bytes = statistics.total_allocated_bytes();
}

} // namespace

struct gossamer_v8_realm {
  std::mutex mutex;
  std::unique_ptr<v8::ArrayBuffer::Allocator> allocator;
  v8::Isolate *isolate = nullptr;
  v8::Global<v8::Context> context;
  v8::Global<v8::FunctionTemplate> event_target_template;
  v8::Global<v8::FunctionTemplate> node_template;
  v8::Global<v8::FunctionTemplate> element_template;
  v8::Global<v8::FunctionTemplate> html_element_template;
  v8::Global<v8::FunctionTemplate> text_template;
  v8::Global<v8::FunctionTemplate> document_template;
  v8::Global<v8::FunctionTemplate> document_fragment_template;
  v8::Global<v8::FunctionTemplate> event_template;
  v8::Global<v8::FunctionTemplate> mouse_event_template;
  v8::Global<v8::FunctionTemplate> pointer_event_template;
  v8::Global<v8::FunctionTemplate> keyboard_event_template;
  v8::Global<v8::FunctionTemplate> input_event_template;
  v8::Global<v8::FunctionTemplate> focus_event_template;
  v8::Global<v8::FunctionTemplate> node_list_template;
  v8::Global<v8::FunctionTemplate> html_collection_template;
  v8::Global<v8::FunctionTemplate> token_list_template;
  v8::Global<v8::FunctionTemplate> dataset_template;
  v8::Global<v8::ObjectTemplate> style_template;
  v8::Global<v8::ObjectTemplate> collection_iterator_template;
  v8::Global<v8::Object> document_wrapper;
  const gossamer_v8_host *active_host = nullptr;
  bool sampling = false;
  bool closed = false;
  bool document_bound = false;
  WrapperKey document_key;
  std::string base_uri;

  std::unordered_map<WrapperKey, WrapperEntry, WrapperKeyHash> wrappers;
  std::vector<WrapperKey> collected_wrappers;
  std::unordered_map<ListenerKey, std::vector<std::unique_ptr<ListenerRecord>>,
                     ListenerKeyHash>
      listeners;
  std::unordered_set<EventWeakData *> events;
  uint64_t next_listener = 1;
  uint32_t dispatch_depth = 0;
  uint64_t event_listener_count = 0;
  uint64_t next_callback = 1;
  std::unordered_map<uint64_t, v8::Global<v8::Function>> callbacks;
  std::unordered_map<uint64_t, uint64_t> timer_callbacks;
  std::unordered_map<uint64_t, uint64_t> callback_timers;

  std::atomic<uint64_t> evaluations{0};
  std::atomic<uint64_t> evaluation_nanos{0};
  std::atomic<uint64_t> microtask_checkpoints{0};
  std::atomic<uint64_t> foreground_tasks{0};
  std::atomic<uint64_t> gc_prologues{0};
  std::atomic<uint64_t> gc_epilogues{0};
  std::atomic<uint64_t> minor_gcs{0};
  std::atomic<uint64_t> major_gcs{0};
  std::atomic<uint64_t> gc_nanos{0};
  std::atomic<uint64_t> gc_started_at{0};
  std::atomic<uint64_t> wrappers_created{0};
  std::atomic<uint64_t> wrapper_cache_hits{0};
  std::atomic<uint64_t> wrappers_collected{0};
  std::atomic<uint64_t> callbacks_created{0};
  std::atomic<uint64_t> callbacks_invoked{0};
  std::atomic<uint64_t> events_dispatched{0};
};

namespace {

struct WrapperWeakData {
  gossamer_v8_realm *realm;
  WrapperKey key;
};

struct EventWeakData {
  gossamer_v8_realm *realm;
  EventState *state;
  v8::Global<v8::Object> object;
};

class HostScope {
public:
  HostScope(gossamer_v8_realm *realm, const gossamer_v8_host *host)
      : realm_(realm), previous_(realm->active_host) {
    realm_->active_host = host;
  }

  ~HostScope() { realm_->active_host = previous_; }

private:
  gossamer_v8_realm *realm_;
  const gossamer_v8_host *previous_;
};

void GCPrologue(v8::Isolate *, v8::GCType, v8::GCCallbackFlags, void *data) {
  auto *realm = static_cast<gossamer_v8_realm *>(data);
  realm->gc_prologues.fetch_add(1, std::memory_order_relaxed);
  realm->gc_started_at.store(MonotonicNanos(), std::memory_order_relaxed);
}

void GCEpilogue(v8::Isolate *, v8::GCType type, v8::GCCallbackFlags,
                void *data) {
  auto *realm = static_cast<gossamer_v8_realm *>(data);
  realm->gc_epilogues.fetch_add(1, std::memory_order_relaxed);
  if ((type & (v8::kGCTypeScavenge | v8::kGCTypeMinorMarkSweep)) != 0)
    realm->minor_gcs.fetch_add(1, std::memory_order_relaxed);
  if ((type & v8::kGCTypeMarkSweepCompact) != 0)
    realm->major_gcs.fetch_add(1, std::memory_order_relaxed);
  uint64_t started =
      realm->gc_started_at.exchange(0, std::memory_order_relaxed);
  if (started != 0)
    realm->gc_nanos.fetch_add(MonotonicNanos() - started,
                              std::memory_order_relaxed);
}

bool RequireRealm(gossamer_v8_realm *realm, char **error_out) {
  if (realm == nullptr || realm->closed || realm->isolate == nullptr) {
    SetError(error_out, "V8 realm is closed");
    return false;
  }
  return true;
}

gossamer_v8_realm *CurrentRealm(v8::Isolate *isolate) {
  return static_cast<gossamer_v8_realm *>(isolate->GetData(0));
}

void ThrowError(v8::Isolate *isolate, const std::string &message) {
  v8::Local<v8::String> rendered;
  if (!v8::String::NewFromUtf8(
           isolate, message.data(), v8::NewStringType::kNormal,
           static_cast<int>(std::min<size_t>(message.size(),
                                             std::numeric_limits<int>::max())))
           .ToLocal(&rendered)) {
    rendered = v8::String::NewFromUtf8Literal(isolate, "host binding failed");
  }
  isolate->ThrowException(v8::Exception::Error(rendered));
}

bool RequireHost(gossamer_v8_realm *realm, std::string *error) {
  if (realm == nullptr || realm->active_host == nullptr ||
      realm->active_host->execution_id == 0) {
    if (error != nullptr)
      *error = "V8 host bindings are unavailable outside a Page task";
    return false;
  }
  return true;
}

bool ReadWrapperKey(v8::Local<v8::Object> object, WrapperKey *key) {
  if (object.IsEmpty() || object->InternalFieldCount() < 2 || key == nullptr)
    return false;
  v8::Local<v8::Data> document_data =
      object->GetInternalField(kNodeDocumentField);
  v8::Local<v8::Data> node_data = object->GetInternalField(kNodeIDField);
  if (!document_data->IsValue() || !node_data->IsValue())
    return false;
  v8::Local<v8::Value> document_value = document_data.As<v8::Value>();
  v8::Local<v8::Value> node_value = node_data.As<v8::Value>();
  if (!document_value->IsBigInt() || !node_value->IsUint32())
    return false;
  bool lossless = false;
  key->document = document_value.As<v8::BigInt>()->Uint64Value(&lossless);
  if (!lossless)
    return false;
  key->node = node_value.As<v8::Uint32>()->Value();
  return true;
}

bool ReadReceiverKey(v8::Isolate *isolate, v8::Local<v8::Object> receiver,
                     WrapperKey *key) {
  if (ReadWrapperKey(receiver, key))
    return true;
  ThrowError(isolate, "DOM method receiver is not a Gossamer node wrapper");
  return false;
}

bool ReadNodeArgument(v8::Isolate *isolate, v8::Local<v8::Value> value,
                      WrapperKey *key, const char *message) {
  if (value->IsObject() && ReadWrapperKey(value.As<v8::Object>(), key))
    return true;
  ThrowError(isolate, message);
  return false;
}

bool StringFromValue(v8::Isolate *isolate, v8::Local<v8::Value> value,
                     std::string *output) {
  v8::Local<v8::String> rendered;
  if (!value->ToString(isolate->GetCurrentContext()).ToLocal(&rendered))
    return false;
  *output = UTF8Value(isolate, rendered);
  return true;
}

bool NewUTF8String(v8::Isolate *isolate, const char *bytes, size_t length,
                   v8::Local<v8::String> *output) {
  if (length > static_cast<size_t>(std::numeric_limits<int>::max()))
    return false;
  return v8::String::NewFromUtf8(isolate, bytes == nullptr ? "" : bytes,
                                 v8::NewStringType::kNormal,
                                 static_cast<int>(length))
      .ToLocal(output);
}

bool ReadStyleKey(v8::Isolate *isolate, v8::Local<v8::Object> receiver,
                  WrapperKey *key) {
  if (receiver.IsEmpty() || receiver->InternalFieldCount() != 1) {
    ThrowError(isolate,
               "CSS method receiver is not a Gossamer style declaration");
    return false;
  }
  v8::Local<v8::Data> node_data = receiver->GetInternalField(kStyleNodeField);
  if (!node_data->IsValue()) {
    ThrowError(isolate, "Gossamer style declaration lost its element");
    return false;
  }
  v8::Local<v8::Value> node_value = node_data.As<v8::Value>();
  if (!node_value->IsObject() ||
      !ReadWrapperKey(node_value.As<v8::Object>(), key)) {
    ThrowError(isolate, "Gossamer style declaration lost its element");
    return false;
  }
  return true;
}

bool ReadFacadeKey(v8::Isolate *isolate, v8::Local<v8::Object> receiver,
                   WrapperKey *key) {
  if (receiver.IsEmpty() ||
      receiver->InternalFieldCount() < kFacadeInternalFieldCount) {
    ThrowError(isolate, "DOM facade receiver is not a Gossamer facade");
    return false;
  }
  v8::Local<v8::Data> node_data = receiver->GetInternalField(kFacadeNodeField);
  if (!node_data->IsValue()) {
    ThrowError(isolate, "Gossamer DOM facade lost its node");
    return false;
  }
  v8::Local<v8::Value> node_value = node_data.As<v8::Value>();
  if (!node_value->IsObject() ||
      !ReadWrapperKey(node_value.As<v8::Object>(), key)) {
    ThrowError(isolate, "Gossamer DOM facade lost its node");
    return false;
  }
  return true;
}

struct NodeMetadata {
  uint8_t type = 0;
  std::string node_name;
  std::string local_name;
  std::string namespace_uri;
  std::string prefix;
  bool connected = false;
};

bool ReadNodeMetadata(gossamer_v8_realm *realm, const WrapperKey &key,
                      NodeMetadata *metadata, std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *node_name = nullptr;
  size_t node_name_length = 0;
  char *local_name = nullptr;
  size_t local_name_length = 0;
  char *namespace_uri = nullptr;
  size_t namespace_uri_length = 0;
  char *prefix = nullptr;
  size_t prefix_length = 0;
  int connected = 0;
  char *host_error = nullptr;
  int ok = realm->active_host->node_metadata(
      realm->active_host->execution_id, key.document, key.node,
      &metadata->type, &node_name, &node_name_length, &local_name,
      &local_name_length, &namespace_uri, &namespace_uri_length, &prefix,
      &prefix_length, &connected, &host_error);
  if (ok == 0) {
    *error = TakeCString(host_error);
    std::free(node_name);
    std::free(local_name);
    std::free(namespace_uri);
    std::free(prefix);
    if (error->empty())
      *error = "reading DOM node metadata failed";
    return false;
  }
  std::free(host_error);
  metadata->node_name.assign(node_name == nullptr ? "" : node_name,
                             node_name_length);
  metadata->local_name.assign(local_name == nullptr ? "" : local_name,
                              local_name_length);
  metadata->namespace_uri.assign(
      namespace_uri == nullptr ? "" : namespace_uri, namespace_uri_length);
  metadata->prefix.assign(prefix == nullptr ? "" : prefix, prefix_length);
  metadata->connected = connected != 0;
  std::free(node_name);
  std::free(local_name);
  std::free(namespace_uri);
  std::free(prefix);
  return true;
}

std::string CSSPropertyNameFromJS(const std::string &name) {
  if (name == "cssFloat")
    return "float";
  if (name.rfind("--", 0) == 0)
    return name;
  std::string result;
  result.reserve(name.size() + 4);
  for (char character : name) {
    if (character >= 'A' && character <= 'Z') {
      result.push_back('-');
      result.push_back(static_cast<char>(character + ('a' - 'A')));
    } else {
      result.push_back(character);
    }
  }
  return result;
}

bool EventTypeFromValue(v8::Isolate *isolate, v8::Local<v8::Value> value,
                        std::string *event_type) {
  if (!value->IsString()) {
    ThrowError(isolate, "event type must be a string");
    return false;
  }
  *event_type = UTF8Value(isolate, value);
  return true;
}
void WrapperCollected(const v8::WeakCallbackInfo<WrapperWeakData> &info) {
  WrapperWeakData *weak = info.GetParameter();
  if (weak == nullptr || weak->realm == nullptr)
    return;
  gossamer_v8_realm *realm = weak->realm;
  auto entry = realm->wrappers.find(weak->key);
  if (entry != realm->wrappers.end() && entry->second.weak == weak) {
    entry->second.object.Reset();
    realm->wrappers.erase(entry);
    realm->collected_wrappers.push_back(weak->key);
    realm->wrappers_collected.fetch_add(1, std::memory_order_relaxed);
  }
  delete weak;
}

v8::MaybeLocal<v8::Object>
GetOrCreateNodeWrapper(gossamer_v8_realm *realm, v8::Local<v8::Context> context,
                       const WrapperKey &key, std::string *error) {
  auto cached = realm->wrappers.find(key);
  if (cached != realm->wrappers.end()) {
    realm->wrapper_cache_hits.fetch_add(1, std::memory_order_relaxed);
    return cached->second.object.Get(realm->isolate);
  }

  NodeMetadata metadata;
  if (!ReadNodeMetadata(realm, key, &metadata, error))
    return {};
  v8::Local<v8::FunctionTemplate> node_template;
  if (metadata.type == 9) {
    node_template = realm->document_template.Get(realm->isolate);
  } else if (metadata.type == 11) {
    node_template = realm->document_fragment_template.Get(realm->isolate);
  } else if (metadata.type == 3) {
    node_template = realm->text_template.Get(realm->isolate);
  } else if (metadata.type == 1) {
    node_template =
        metadata.namespace_uri == "http://www.w3.org/1999/xhtml"
            ? realm->html_element_template.Get(realm->isolate)
            : realm->element_template.Get(realm->isolate);
  } else {
    node_template = realm->node_template.Get(realm->isolate);
  }
  v8::Local<v8::Object> object;
  if (!node_template->InstanceTemplate()->NewInstance(context).ToLocal(&object))
    return {};
  object->SetInternalField(
      kNodeDocumentField,
      v8::BigInt::NewFromUnsigned(realm->isolate, key.document));
  object->SetInternalField(
      kNodeIDField, v8::Integer::NewFromUnsigned(realm->isolate, key.node));

  if (!RequireHost(realm, error))
    return {};
  char *host_error = nullptr;
  if (realm->active_host->retain_node_wrapper(
          realm->active_host->execution_id, key.document, key.node,
          &host_error) == 0) {
    if (error != nullptr) {
      *error = TakeCString(host_error);
      if (error->empty())
        *error = "Go host rejected the DOM wrapper lifetime";
    } else {
      std::free(host_error);
    }
    return {};
  }
  std::free(host_error);

  // A wrapper can be collected and recreated during the same V8 entry before
  // Go drains the weak-finalization queue. The successfully retained wrapper
  // supersedes that pending release for the same numeric identity.
  realm->collected_wrappers.erase(
      std::remove(realm->collected_wrappers.begin(),
                  realm->collected_wrappers.end(), key),
      realm->collected_wrappers.end());

  auto inserted = realm->wrappers.try_emplace(key);
  WrapperEntry &entry = inserted.first->second;
  entry.weak = new WrapperWeakData{realm, key};
  entry.object.Reset(realm->isolate, object);
  entry.object.SetWeak(entry.weak, WrapperCollected,
                       v8::WeakCallbackType::kParameter);
  realm->wrappers_created.fetch_add(1, std::memory_order_relaxed);
  return object;
}

bool EnsureDocumentBinding(gossamer_v8_realm *realm,
                           v8::Local<v8::Context> context,
                           std::string *error) {
  if (realm->document_bound || realm->active_host == nullptr ||
      realm->active_host->execution_id == 0)
    return true;
  uint64_t document = 0;
  uint32_t node = 0;
  char *base_uri = nullptr;
  size_t base_uri_length = 0;
  int found = 0;
  char *host_error = nullptr;
  if (realm->active_host->document_metadata(
          realm->active_host->execution_id, &document, &node, &base_uri,
          &base_uri_length, &found, &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(base_uri);
    if (error->empty())
      *error = "reading document metadata failed";
    return false;
  }
  std::free(host_error);
  if (found == 0) {
    std::free(base_uri);
    return true;
  }
  realm->document_key = WrapperKey{document, node};
  realm->base_uri.assign(base_uri == nullptr ? "" : base_uri, base_uri_length);
  std::free(base_uri);
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, context, realm->document_key, error)
           .ToLocal(&wrapper))
    return false;
  if (!context->Global()
           ->DefineOwnProperty(
               context,
               v8::String::NewFromUtf8Literal(realm->isolate, "document"),
               wrapper,
               static_cast<v8::PropertyAttribute>(v8::ReadOnly |
                                                  v8::DontDelete))
           .FromMaybe(false)) {
    *error = "V8 failed to install the canonical document wrapper";
    return false;
  }
  realm->document_wrapper.Reset(realm->isolate, wrapper);
  realm->document_bound = true;
  return true;
}

bool ReadCanonicalDocument(gossamer_v8_realm *realm,
                           v8::Local<v8::Context> context,
                           v8::Local<v8::Value> *document) {
  if (!realm->document_bound || realm->document_wrapper.IsEmpty())
    return false;
  *document = realm->document_wrapper.Get(realm->isolate);
  return true;
}

uint64_t StoreOneShotCallback(gossamer_v8_realm *realm,
                              v8::Local<v8::Function> function) {
  uint64_t callback = realm->next_callback++;
  while (callback == 0 ||
         realm->callbacks.find(callback) != realm->callbacks.end())
    callback = realm->next_callback++;
  auto inserted = realm->callbacks.try_emplace(callback);
  inserted.first->second.Reset(realm->isolate, function);
  realm->callbacks_created.fetch_add(1, std::memory_order_relaxed);
  return callback;
}

void RemoveCallback(gossamer_v8_realm *realm, uint64_t callback) {
  auto timer = realm->callback_timers.find(callback);
  if (timer != realm->callback_timers.end()) {
    realm->timer_callbacks.erase(timer->second);
    realm->callback_timers.erase(timer);
  }
  auto entry = realm->callbacks.find(callback);
  if (entry != realm->callbacks.end()) {
    entry->second.Reset();
    realm->callbacks.erase(entry);
  }
}

bool QueuePublishedCallback(gossamer_v8_realm *realm, uint64_t callback,
                            bool microtask, std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  int ok = microtask
               ? realm->active_host->queue_microtask(
                     realm->active_host->execution_id, callback, &host_error)
               : realm->active_host->queue_callback(
                     realm->active_host->execution_id, callback, &host_error);
  if (ok == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "Go host rejected the callback";
    return false;
  }
  std::free(host_error);
  return true;
}

void DocumentGetElementByID(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (info.Length() == 0) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::String> value;
  if (!info[0]->ToString(context).ToLocal(&value))
    return;
  std::string id = UTF8Value(isolate, value);
  uint64_t document = 0;
  uint32_t node = 0;
  int found = 0;
  char *host_error = nullptr;
  if (realm->active_host->get_element_by_id(realm->active_host->execution_id,
                                            id.data(), id.size(), &document,
                                            &node, &found, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "getElementById failed" : error);
    return;
  }
  std::free(host_error);
  if (found == 0) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, context, WrapperKey{document, node},
                              &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error.empty()
                            ? "V8 failed to allocate a DOM node wrapper"
                            : error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void DocumentCreateNode(const v8::FunctionCallbackInfo<v8::Value> &info,
                        bool element) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  std::string input;
  if (info.Length() == 0) {
    if (element) {
      ThrowError(isolate, "createElement requires a tag name");
      return;
    }
  } else if (!StringFromValue(isolate, info[0], &input)) {
    return;
  }
  uint64_t document = 0;
  uint32_t node = 0;
  char *host_error = nullptr;
  int ok = element ? realm->active_host->create_element(
                         realm->active_host->execution_id, input.data(),
                         input.size(), &document, &node, &host_error)
                   : realm->active_host->create_text_node(
                         realm->active_host->execution_id, input.data(),
                         input.size(), &document, &node, &host_error);
  if (ok == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? (element ? "createElement failed"
                                                 : "createTextNode failed")
                                      : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, isolate->GetCurrentContext(),
                              WrapperKey{document, node}, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error.empty()
                            ? "V8 failed to allocate a DOM node wrapper"
                            : error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void DocumentCreateElement(const v8::FunctionCallbackInfo<v8::Value> &info) {
  DocumentCreateNode(info, true);
}

void DocumentCreateElementNS(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (info.Length() < 2) {
    ThrowError(isolate, "createElementNS requires a namespace and qualified name");
    return;
  }
  std::string namespace_uri;
  std::string qualified_name;
  if (!info[0]->IsNull() && !StringFromValue(isolate, info[0], &namespace_uri))
    return;
  if (!StringFromValue(isolate, info[1], &qualified_name))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  uint64_t document = 0;
  uint32_t node = 0;
  char *host_error = nullptr;
  if (realm->active_host->create_element_ns(
          realm->active_host->execution_id, namespace_uri.data(),
          namespace_uri.size(), qualified_name.data(), qualified_name.size(),
          &document, &node, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "createElementNS failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, isolate->GetCurrentContext(),
                              WrapperKey{document, node}, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error.empty()
                            ? "V8 failed to allocate a namespaced DOM wrapper"
                            : error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void DocumentCreateTextNode(const v8::FunctionCallbackInfo<v8::Value> &info) {
  DocumentCreateNode(info, false);
}

void DocumentCreateDocumentFragment(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  uint64_t document = 0;
  uint32_t node = 0;
  char *host_error = nullptr;
  if (realm->active_host->create_document_fragment(
          realm->active_host->execution_id, &document, &node,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "createDocumentFragment failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, isolate->GetCurrentContext(),
                              WrapperKey{document, node}, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void NodeCloneNode(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  uint64_t clone_document = 0;
  uint32_t clone_node = 0;
  char *host_error = nullptr;
  bool deep = info.Length() != 0 && info[0]->BooleanValue(isolate);
  if (realm->active_host->clone_node(
          realm->active_host->execution_id, key.document, key.node,
          deep ? 1 : 0, &clone_document, &clone_node, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "cloneNode failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, isolate->GetCurrentContext(),
                              WrapperKey{clone_document, clone_node}, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void NodeTextContentGetter(v8::Local<v8::Name>,
                           const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *value = nullptr;
  size_t value_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->text_content(realm->active_host->execution_id,
                                       key.document, key.node, &value,
                                       &value_length, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(value);
    ThrowError(isolate, error.empty() ? "reading textContent failed" : error);
    return;
  }
  std::free(host_error);
  if (value_length > static_cast<size_t>(std::numeric_limits<int>::max())) {
    std::free(value);
    ThrowError(isolate, "textContent exceeds V8's supported string length");
    return;
  }
  v8::Local<v8::String> result;
  const char *bytes = value == nullptr ? "" : value;
  bool allocated =
      v8::String::NewFromUtf8(isolate, bytes, v8::NewStringType::kNormal,
                              static_cast<int>(value_length))
          .ToLocal(&result);
  std::free(value);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate textContent");
    return;
  }
  info.GetReturnValue().Set(result);
}

void NodeTextContentSetter(v8::Local<v8::Name>, v8::Local<v8::Value> value,
                           const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  v8::Local<v8::String> rendered;
  if (!value->ToString(isolate->GetCurrentContext()).ToLocal(&rendered)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string text = UTF8Value(isolate, rendered);
  char *host_error = nullptr;
  if (realm->active_host->set_text_content(realm->active_host->execution_id,
                                           key.document, key.node, text.data(),
                                           text.size(), &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "setting textContent failed" : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

void NodeAppendChild(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey parent;
  WrapperKey child;
  if (!ReadReceiverKey(isolate, info.This(), &parent))
    return;
  if (info.Length() == 0 ||
      !ReadNodeArgument(isolate, info[0], &child,
                        "appendChild requires a Gossamer node"))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->append_child(
          realm->active_host->execution_id, parent.document, parent.node,
          child.document, child.node, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "appendChild failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(info[0]);
}

void NodeInsertBefore(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey parent;
  WrapperKey child;
  WrapperKey reference;
  if (!ReadReceiverKey(isolate, info.This(), &parent))
    return;
  if (info.Length() < 2 ||
      !ReadNodeArgument(isolate, info[0], &child,
                        "insertBefore requires a Gossamer child node"))
    return;
  if (!info[1]->IsNull() &&
      !ReadNodeArgument(
          isolate, info[1], &reference,
          "insertBefore reference must be a Gossamer node or null"))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->insert_before(
          realm->active_host->execution_id, parent.document, parent.node,
          child.document, child.node, reference.document, reference.node,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "insertBefore failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(info[0]);
}

void NodeRemoveChild(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey parent;
  WrapperKey child;
  if (!ReadReceiverKey(isolate, info.This(), &parent))
    return;
  if (info.Length() == 0 ||
      !ReadNodeArgument(isolate, info[0], &child,
                        "removeChild requires a Gossamer node"))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->remove_child(
          realm->active_host->execution_id, parent.document, parent.node,
          child.document, child.node, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "removeChild failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(info[0]);
}

void NodeGetAttribute(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  std::string name;
  if (info.Length() == 0 || !StringFromValue(isolate, info[0], &name))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *value = nullptr;
  size_t value_length = 0;
  int found = 0;
  char *host_error = nullptr;
  if (realm->active_host->get_attribute(
          realm->active_host->execution_id, key.document, key.node, name.data(),
          name.size(), &value, &value_length, &found, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(value);
    ThrowError(isolate, error.empty() ? "getAttribute failed" : error);
    return;
  }
  std::free(host_error);
  if (found == 0) {
    std::free(value);
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  if (value_length > static_cast<size_t>(std::numeric_limits<int>::max())) {
    std::free(value);
    ThrowError(isolate, "attribute value exceeds V8's supported string length");
    return;
  }
  v8::Local<v8::String> result;
  const char *bytes = value == nullptr ? "" : value;
  bool allocated =
      v8::String::NewFromUtf8(isolate, bytes, v8::NewStringType::kNormal,
                              static_cast<int>(value_length))
          .ToLocal(&result);
  std::free(value);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate an attribute value");
    return;
  }
  info.GetReturnValue().Set(result);
}

void NodeSetAttribute(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  std::string name;
  std::string value;
  if (info.Length() < 2 || !StringFromValue(isolate, info[0], &name) ||
      !StringFromValue(isolate, info[1], &value))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->set_attribute(
          realm->active_host->execution_id, key.document, key.node, name.data(),
          name.size(), value.data(), value.size(), &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "setAttribute failed" : error);
    return;
  }
  std::free(host_error);
}

void NodeRemoveAttribute(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  std::string name;
  if (info.Length() == 0 || !StringFromValue(isolate, info[0], &name))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->remove_attribute(realm->active_host->execution_id,
                                           key.document, key.node, name.data(),
                                           name.size(), &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "removeAttribute failed" : error);
    return;
  }
  std::free(host_error);
}

void NodeMetadataGetter(v8::Local<v8::Name> property,
                        const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  NodeMetadata metadata;
  std::string error;
  if (!ReadNodeMetadata(realm, key, &metadata, &error)) {
    ThrowError(isolate, error);
    return;
  }
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  if (name == "nodeType") {
    info.GetReturnValue().Set(v8::Integer::NewFromUnsigned(isolate,
                                                           metadata.type));
    return;
  }
  if (name == "isConnected") {
    info.GetReturnValue().Set(v8::Boolean::New(isolate, metadata.connected));
    return;
  }
  if (name == "localName") {
    if (metadata.type != 1) {
      info.GetReturnValue().Set(v8::Null(isolate));
      return;
    }
    v8::Local<v8::String> value;
    if (!NewUTF8String(isolate, metadata.local_name.data(),
                       metadata.local_name.size(), &value)) {
      ThrowError(isolate, "V8 failed to allocate localName");
      return;
    }
    info.GetReturnValue().Set(value);
    return;
  }
  if (name == "namespaceURI") {
    if (metadata.type != 1 || metadata.namespace_uri.empty()) {
      info.GetReturnValue().Set(v8::Null(isolate));
      return;
    }
    v8::Local<v8::String> value;
    if (!NewUTF8String(isolate, metadata.namespace_uri.data(),
                       metadata.namespace_uri.size(), &value)) {
      ThrowError(isolate, "V8 failed to allocate namespaceURI");
      return;
    }
    info.GetReturnValue().Set(value);
    return;
  }
  if (name == "prefix") {
    if (metadata.type != 1 || metadata.prefix.empty()) {
      info.GetReturnValue().Set(v8::Null(isolate));
      return;
    }
    v8::Local<v8::String> value;
    if (!NewUTF8String(isolate, metadata.prefix.data(), metadata.prefix.size(),
                       &value)) {
      ThrowError(isolate, "V8 failed to allocate prefix");
      return;
    }
    info.GetReturnValue().Set(value);
    return;
  }
  if (name == "tagName" && metadata.type != 1) {
    info.GetReturnValue().Set(v8::Undefined(isolate));
    return;
  }
  v8::Local<v8::String> value;
  if (!NewUTF8String(isolate, metadata.node_name.data(),
                     metadata.node_name.size(), &value)) {
    ThrowError(isolate, "V8 failed to allocate nodeName");
    return;
  }
  info.GetReturnValue().Set(value);
}

void NodeOwnerDocumentGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  if (!realm->document_bound || key == realm->document_key) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  v8::Local<v8::Value> document;
  if (!ReadCanonicalDocument(realm, isolate->GetCurrentContext(), &document)) {
    ThrowError(isolate, "Gossamer document wrapper is unavailable");
    return;
  }
  info.GetReturnValue().Set(document);
}

void NodeBaseURIGetter(v8::Local<v8::Name>,
                       const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::String> value;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (!NewUTF8String(isolate, realm->base_uri.data(), realm->base_uri.size(),
                     &value)) {
    ThrowError(isolate, "V8 failed to allocate baseURI");
    return;
  }
  info.GetReturnValue().Set(value);
}

void DocumentDefaultViewGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  info.GetReturnValue().Set(info.GetIsolate()->GetCurrentContext()->Global());
}

bool RelationFromProperty(const std::string &name, uint8_t *relation) {
  if (name == "parentNode")
    *relation = 1;
  else if (name == "parentElement")
    *relation = 2;
  else if (name == "firstChild")
    *relation = 3;
  else if (name == "lastChild")
    *relation = 4;
  else if (name == "previousSibling")
    *relation = 5;
  else if (name == "nextSibling")
    *relation = 6;
  else if (name == "firstElementChild")
    *relation = 7;
  else if (name == "lastElementChild")
    *relation = 8;
  else if (name == "previousElementSibling")
    *relation = 9;
  else if (name == "nextElementSibling")
    *relation = 10;
  else if (name == "documentElement")
    *relation = 11;
  else if (name == "head")
    *relation = 12;
  else if (name == "body")
    *relation = 13;
  else
    return false;
  return true;
}

void NodeRelationGetter(v8::Local<v8::Name> property,
                        const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  uint8_t relation = 0;
  if (!RelationFromProperty(UTF8Value(isolate, property.As<v8::Value>()),
                            &relation)) {
    ThrowError(isolate, "unknown DOM traversal property");
    return;
  }
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  uint32_t related_node = 0;
  int found = 0;
  char *host_error = nullptr;
  if (realm->active_host->related_node(
          realm->active_host->execution_id, key.document, key.node, relation,
          &related_node, &found, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "DOM traversal failed" : error);
    return;
  }
  std::free(host_error);
  if (found == 0) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, isolate->GetCurrentContext(),
                              WrapperKey{key.document, related_node}, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error.empty() ? "V8 failed to wrap a related DOM node"
                                      : error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

bool ReadChildNodes(gossamer_v8_realm *realm, const WrapperKey &key,
                    bool elements_only, std::vector<uint32_t> *nodes,
                    std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  uint32_t *host_nodes = nullptr;
  size_t count = 0;
  char *host_error = nullptr;
  if (realm->active_host->child_nodes(
          realm->active_host->execution_id, key.document, key.node,
          elements_only ? 1 : 0, &host_nodes, &count, &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(host_nodes);
    if (error->empty())
      *error = "reading DOM child nodes failed";
    return false;
  }
  std::free(host_error);
  if (count == 0) {
    nodes->clear();
  } else {
    nodes->assign(host_nodes, host_nodes + count);
  }
  std::free(host_nodes);
  return true;
}

bool ReadAttributeNames(gossamer_v8_realm *realm, const WrapperKey &key,
                        std::vector<std::string> *names,
                        std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  size_t count = 0;
  char *host_error = nullptr;
  if (realm->active_host->attribute_count(
          realm->active_host->execution_id, key.document, key.node, &count,
          &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "reading DOM attribute count failed";
    return false;
  }
  std::free(host_error);
  names->clear();
  names->reserve(count);
  for (size_t index = 0; index < count; ++index) {
    char *name = nullptr;
    size_t name_length = 0;
    int found = 0;
    host_error = nullptr;
    if (realm->active_host->attribute_name(
            realm->active_host->execution_id, key.document, key.node, index,
            &name, &name_length, &found, &host_error) == 0) {
      *error = TakeCString(host_error);
      std::free(name);
      if (error->empty())
        *error = "reading DOM attribute name failed";
      return false;
    }
    std::free(host_error);
    if (found != 0)
      names->emplace_back(name == nullptr ? "" : name, name_length);
    std::free(name);
  }
  return true;
}

bool ReadAttribute(gossamer_v8_realm *realm, const WrapperKey &key,
                   const std::string &name, std::string *value, bool *found,
                   std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_value = nullptr;
  size_t value_length = 0;
  int host_found = 0;
  char *host_error = nullptr;
  if (realm->active_host->get_attribute(
          realm->active_host->execution_id, key.document, key.node,
          name.data(), name.size(), &host_value, &value_length, &host_found,
          &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(host_value);
    if (error->empty())
      *error = "reading DOM attribute failed";
    return false;
  }
  std::free(host_error);
  *found = host_found != 0;
  value->assign(host_value == nullptr ? "" : host_value,
                *found ? value_length : 0);
  std::free(host_value);
  return true;
}

bool WriteAttribute(gossamer_v8_realm *realm, const WrapperKey &key,
                    const std::string &name, const std::string &value,
                    std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->set_attribute(
          realm->active_host->execution_id, key.document, key.node,
          name.data(), name.size(), value.data(), value.size(),
          &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "writing DOM attribute failed";
    return false;
  }
  std::free(host_error);
  return true;
}

bool DeleteAttribute(gossamer_v8_realm *realm, const WrapperKey &key,
                     const std::string &name, std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->remove_attribute(
          realm->active_host->execution_id, key.document, key.node,
          name.data(), name.size(), &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "removing DOM attribute failed";
    return false;
  }
  std::free(host_error);
  return true;
}

bool IsASCIIWhitespace(char character) {
  return character == ' ' || character == '\t' || character == '\n' ||
         character == '\r' || character == '\f';
}

std::vector<std::string> ParseClassTokens(const std::string &value) {
  std::vector<std::string> tokens;
  std::unordered_set<std::string> seen;
  size_t index = 0;
  while (index < value.size()) {
    while (index < value.size() && IsASCIIWhitespace(value[index]))
      ++index;
    size_t start = index;
    while (index < value.size() && !IsASCIIWhitespace(value[index]))
      ++index;
    if (start == index)
      continue;
    std::string token = value.substr(start, index - start);
    if (seen.insert(token).second)
      tokens.push_back(std::move(token));
  }
  return tokens;
}

std::string SerializeClassTokens(const std::vector<std::string> &tokens) {
  std::string result;
  for (const std::string &token : tokens) {
    if (!result.empty())
      result.push_back(' ');
    result += token;
  }
  return result;
}

bool ValidateClassToken(v8::Isolate *isolate, const std::string &token) {
  if (token.empty()) {
    ThrowError(isolate, "classList token must not be empty");
    return false;
  }
  if (std::any_of(token.begin(), token.end(), IsASCIIWhitespace)) {
    ThrowError(isolate, "classList token must not contain ASCII whitespace");
    return false;
  }
  return true;
}

bool ReadClassTokens(gossamer_v8_realm *realm, const WrapperKey &key,
                     std::vector<std::string> *tokens, std::string *error) {
  std::string value;
  bool found = false;
  if (!ReadAttribute(realm, key, "class", &value, &found, error))
    return false;
  *tokens = ParseClassTokens(found ? value : "");
  return true;
}

bool WriteClassTokens(gossamer_v8_realm *realm, const WrapperKey &key,
                      const std::vector<std::string> &tokens,
                      std::string *error) {
  if (tokens.empty())
    return DeleteAttribute(realm, key, "class", error);
  return WriteAttribute(realm, key, "class", SerializeClassTokens(tokens),
                        error);
}

bool DatasetNameFromAttribute(const std::string &attribute,
                              std::string *property) {
  if (attribute.rfind("data-", 0) != 0)
    return false;
  property->clear();
  for (size_t index = 5; index < attribute.size(); ++index) {
    char character = attribute[index];
    if (character == '-' && index + 1 < attribute.size() &&
        attribute[index + 1] >= 'a' && attribute[index + 1] <= 'z') {
      property->push_back(static_cast<char>(attribute[++index] - 'a' + 'A'));
    } else {
      property->push_back(character);
    }
  }
  return true;
}

bool DatasetAttributeFromName(const std::string &property,
                              std::string *attribute) {
  for (size_t index = 0; index + 1 < property.size(); ++index) {
    if (property[index] == '-' && property[index + 1] >= 'a' &&
        property[index + 1] <= 'z')
      return false;
  }
  *attribute = "data-";
  for (char character : property) {
    if (character >= 'A' && character <= 'Z') {
      attribute->push_back('-');
      attribute->push_back(static_cast<char>(character - 'A' + 'a'));
    } else {
      attribute->push_back(character);
    }
  }
  return true;
}

void NodeChildrenGetter(v8::Local<v8::Name> property,
                        const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  v8::Local<v8::Object> node = info.Holder();
  if (!ReadReceiverKey(isolate, node, &key))
    return;
  bool elements_only =
      UTF8Value(isolate, property.As<v8::Value>()) == "children";
  int field = elements_only ? kNodeChildrenField : kNodeChildNodesField;
  v8::Local<v8::Data> cached_data = node->GetInternalField(field);
  if (cached_data->IsValue()) {
    v8::Local<v8::Value> cached = cached_data.As<v8::Value>();
    if (cached->IsObject()) {
      info.GetReturnValue().Set(cached);
      return;
    }
  }
  v8::Local<v8::FunctionTemplate> facade_template =
      elements_only ? realm->html_collection_template.Get(isolate)
                    : realm->node_list_template.Get(isolate);
  v8::Local<v8::Object> facade;
  if (!facade_template->InstanceTemplate()
           ->NewInstance(isolate->GetCurrentContext())
           .ToLocal(&facade)) {
    ThrowError(isolate, "V8 failed to allocate a live child collection");
    return;
  }
  facade->SetInternalField(kFacadeNodeField, node);
  node->SetInternalField(field, facade);
  info.GetReturnValue().Set(facade);
}

void NodeChildElementCountGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  std::vector<uint32_t> nodes;
  std::string error;
  if (!ReadChildNodes(realm, key, true, &nodes, &error)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(v8::Integer::NewFromUnsigned(
      isolate, static_cast<uint32_t>(nodes.size())));
}

FacadeKind FacadeKindFromData(v8::Local<v8::Value> data) {
  if (data.IsEmpty() || !data->IsInt32())
    return FacadeKind::NodeList;
  return static_cast<FacadeKind>(data.As<v8::Int32>()->Value());
}

bool ReadStaticNodeListBacking(v8::Local<v8::Object> facade,
                               v8::Local<v8::Array> *backing) {
  if (facade.IsEmpty() ||
      facade->InternalFieldCount() < kFacadeInternalFieldCount)
    return false;
  v8::Local<v8::Data> data = facade->GetInternalField(kFacadeBackingField);
  if (!data->IsValue() || !data.As<v8::Value>()->IsArray())
    return false;
  *backing = data.As<v8::Value>().As<v8::Array>();
  return true;
}

bool ReadFacadeLength(gossamer_v8_realm *realm, v8::Isolate *isolate,
                      v8::Local<v8::Object> facade, FacadeKind kind,
                      size_t *length, std::string *error) {
  v8::Local<v8::Array> backing;
  if (kind == FacadeKind::NodeList &&
      ReadStaticNodeListBacking(facade, &backing)) {
    *length = backing->Length();
    return true;
  }
  WrapperKey key;
  if (!ReadFacadeKey(isolate, facade, &key))
    return false;
  if (kind == FacadeKind::ClassList) {
    std::vector<std::string> tokens;
    if (!ReadClassTokens(realm, key, &tokens, error))
      return false;
    *length = tokens.size();
    return true;
  }
  std::vector<uint32_t> nodes;
  if (!ReadChildNodes(realm, key, kind == FacadeKind::HTMLCollection, &nodes,
                      error))
    return false;
  *length = nodes.size();
  return true;
}

v8::MaybeLocal<v8::Value>
ReadFacadeValue(gossamer_v8_realm *realm, v8::Local<v8::Context> context,
                v8::Local<v8::Object> facade, FacadeKind kind, uint32_t index,
                bool *found, std::string *error) {
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  v8::Local<v8::Array> backing;
  if (kind == FacadeKind::NodeList &&
      ReadStaticNodeListBacking(facade, &backing)) {
    *found = index < backing->Length();
    if (!*found)
      return v8::Undefined(isolate);
    return backing->Get(context, index);
  }
  WrapperKey key;
  if (!ReadFacadeKey(isolate, facade, &key))
    return {};
  if (kind == FacadeKind::ClassList) {
    std::vector<std::string> tokens;
    if (!ReadClassTokens(realm, key, &tokens, error))
      return {};
    *found = index < tokens.size();
    if (!*found)
      return v8::Undefined(isolate);
    v8::Local<v8::String> value;
    if (!NewUTF8String(isolate, tokens[index].data(), tokens[index].size(),
                       &value))
      return {};
    return value;
  }
  std::vector<uint32_t> nodes;
  if (!ReadChildNodes(realm, key, kind == FacadeKind::HTMLCollection, &nodes,
                      error))
    return {};
  *found = index < nodes.size();
  if (!*found)
    return v8::Undefined(isolate);
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, context,
                              WrapperKey{key.document, nodes[index]}, error)
           .ToLocal(&wrapper))
    return {};
  return wrapper;
}

void FacadeLengthGetter(v8::Local<v8::Name>,
                        const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  size_t length = 0;
  std::string error;
  if (!ReadFacadeLength(CurrentRealm(isolate), isolate, info.Holder(),
                        FacadeKindFromData(info.Data()), &length, &error)) {
    if (!isolate->HasPendingException())
      ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(v8::Number::New(isolate, length));
}

v8::Intercepted FacadeIndexedGetter(
    uint32_t index, const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  bool found = false;
  std::string error;
  v8::Local<v8::Value> value;
  if (!ReadFacadeValue(CurrentRealm(isolate), isolate->GetCurrentContext(),
                       info.Holder(),
                       FacadeKindFromData(info.Data()), index, &found, &error)
           .ToLocal(&value)) {
    ThrowError(isolate, error.empty() ? "reading live DOM facade failed"
                                      : error);
    return v8::Intercepted::kYes;
  }
  if (!found)
    return v8::Intercepted::kNo;
  info.GetReturnValue().Set(value);
  return v8::Intercepted::kYes;
}

v8::Intercepted FacadeIndexedQuery(
    uint32_t index, const v8::PropertyCallbackInfo<v8::Integer> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  size_t length = 0;
  std::string error;
  if (!ReadFacadeLength(CurrentRealm(isolate), isolate, info.Holder(),
                        FacadeKindFromData(info.Data()), &length, &error)) {
    if (!isolate->HasPendingException())
      ThrowError(isolate, error);
    return v8::Intercepted::kYes;
  }
  if (index >= length)
    return v8::Intercepted::kNo;
  info.GetReturnValue().Set(v8::None);
  return v8::Intercepted::kYes;
}

void FacadeIndexedEnumerator(
    const v8::PropertyCallbackInfo<v8::Array> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  size_t length = 0;
  std::string error;
  if (!ReadFacadeLength(CurrentRealm(isolate), isolate, info.Holder(),
                        FacadeKindFromData(info.Data()), &length, &error)) {
    if (!isolate->HasPendingException())
      ThrowError(isolate, error);
    return;
  }
  if (length > static_cast<size_t>(std::numeric_limits<int>::max())) {
    ThrowError(isolate, "DOM facade exceeds V8's enumeration limit");
    return;
  }
  v8::Local<v8::Array> indices =
      v8::Array::New(isolate, static_cast<int>(length));
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  for (size_t index = 0; index < length; ++index) {
    if (!indices
             ->Set(context, static_cast<uint32_t>(index),
                   v8::Integer::NewFromUnsigned(isolate,
                                                static_cast<uint32_t>(index)))
             .FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to enumerate DOM facade indices");
      return;
    }
  }
  info.GetReturnValue().Set(indices);
}

void FacadeItem(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  uint32_t index = 0;
  if (info.Length() != 0 &&
      !info[0]->Uint32Value(isolate->GetCurrentContext()).To(&index))
    return;
  bool found = false;
  std::string error;
  v8::Local<v8::Value> value;
  if (!ReadFacadeValue(CurrentRealm(isolate), isolate->GetCurrentContext(),
                       info.This(),
                       FacadeKindFromData(info.Data()), index, &found, &error)
           .ToLocal(&value)) {
    ThrowError(isolate, error.empty() ? "reading DOM facade item failed"
                                      : error);
    return;
  }
  info.GetReturnValue().Set(found ? value : v8::Null(isolate));
}

bool FindNamedCollectionItem(gossamer_v8_realm *realm, const WrapperKey &key,
                             const std::string &name, uint32_t *node,
                             bool *found, std::string *error) {
  std::vector<uint32_t> nodes;
  if (!ReadChildNodes(realm, key, true, &nodes, error))
    return false;
  for (uint32_t candidate : nodes) {
    WrapperKey candidate_key{key.document, candidate};
    std::string value;
    bool present = false;
    if (!ReadAttribute(realm, candidate_key, "id", &value, &present, error))
      return false;
    if (present && value == name) {
      *node = candidate;
      *found = true;
      return true;
    }
    if (!ReadAttribute(realm, candidate_key, "name", &value, &present, error))
      return false;
    if (present && value == name) {
      *node = candidate;
      *found = true;
      return true;
    }
  }
  *found = false;
  return true;
}

void HTMLCollectionNamedItem(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.This(), &key))
    return;
  std::string name;
  if (info.Length() == 0 || !StringFromValue(isolate, info[0], &name))
    return;
  uint32_t node = 0;
  bool found = false;
  std::string error;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (!FindNamedCollectionItem(realm, key, name, &node, &found, &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (!found) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, isolate->GetCurrentContext(),
                              WrapperKey{key.document, node}, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

v8::Intercepted HTMLCollectionNamedGetter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Value> &info) {
  if (!property->IsString())
    return v8::Intercepted::kNo;
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.Holder(), &key))
    return v8::Intercepted::kYes;
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  uint32_t node = 0;
  bool found = false;
  std::string error;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (!FindNamedCollectionItem(realm, key, name, &node, &found, &error)) {
    ThrowError(isolate, error);
    return v8::Intercepted::kYes;
  }
  if (!found)
    return v8::Intercepted::kNo;
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, isolate->GetCurrentContext(),
                              WrapperKey{key.document, node}, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return v8::Intercepted::kYes;
  }
  info.GetReturnValue().Set(wrapper);
  return v8::Intercepted::kYes;
}

v8::Intercepted HTMLCollectionNamedQuery(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Integer> &info) {
  if (!property->IsString())
    return v8::Intercepted::kNo;
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.Holder(), &key))
    return v8::Intercepted::kYes;
  uint32_t node = 0;
  bool found = false;
  std::string error;
  if (!FindNamedCollectionItem(CurrentRealm(isolate), key,
                               UTF8Value(isolate, property.As<v8::Value>()),
                               &node, &found, &error)) {
    ThrowError(isolate, error);
    return v8::Intercepted::kYes;
  }
  if (!found)
    return v8::Intercepted::kNo;
  info.GetReturnValue().Set(v8::None);
  return v8::Intercepted::kYes;
}

void HTMLCollectionNamedEnumerator(
    const v8::PropertyCallbackInfo<v8::Array> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.Holder(), &key))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::vector<uint32_t> nodes;
  std::string error;
  if (!ReadChildNodes(realm, key, true, &nodes, &error)) {
    ThrowError(isolate, error);
    return;
  }
  std::vector<std::string> names;
  std::unordered_set<std::string> seen;
  for (uint32_t node : nodes) {
    WrapperKey candidate{key.document, node};
    for (const char *attribute : {"id", "name"}) {
      std::string value;
      bool found = false;
      if (!ReadAttribute(realm, candidate, attribute, &value, &found, &error)) {
        ThrowError(isolate, error);
        return;
      }
      if (found && !value.empty() && seen.insert(value).second)
        names.push_back(std::move(value));
    }
  }
  if (names.size() > static_cast<size_t>(std::numeric_limits<int>::max())) {
    ThrowError(isolate, "HTMLCollection exceeds V8's enumeration limit");
    return;
  }
  v8::Local<v8::Array> result =
      v8::Array::New(isolate, static_cast<int>(names.size()));
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  for (size_t index = 0; index < names.size(); ++index) {
    v8::Local<v8::String> value;
    if (!NewUTF8String(isolate, names[index].data(), names[index].size(),
                       &value) ||
        !result->Set(context, static_cast<uint32_t>(index), value)
             .FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to enumerate HTMLCollection names");
      return;
    }
  }
  info.GetReturnValue().Set(result);
}

bool SetIteratorResult(v8::Isolate *isolate, v8::Local<v8::Value> value,
                       bool done,
                       const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Object> result = v8::Object::New(isolate);
  if (!result
           ->Set(context, v8::String::NewFromUtf8Literal(isolate, "value"),
                 value)
           .FromMaybe(false) ||
      !result
           ->Set(context, v8::String::NewFromUtf8Literal(isolate, "done"),
                 v8::Boolean::New(isolate, done))
           .FromMaybe(false)) {
    ThrowError(isolate, "V8 failed to allocate a DOM iterator result");
    return false;
  }
  info.GetReturnValue().Set(result);
  return true;
}

void CollectionIteratorSelf(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  info.GetReturnValue().Set(info.This());
}

void CollectionIteratorNext(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Object> iterator = info.This();
  if (iterator.IsEmpty() || iterator->InternalFieldCount() != 4) {
    ThrowError(isolate, "DOM iterator receiver is not a Gossamer iterator");
    return;
  }
  v8::Local<v8::Data> facade_data =
      iterator->GetInternalField(kIteratorFacadeField);
  v8::Local<v8::Data> index_data =
      iterator->GetInternalField(kIteratorIndexField);
  v8::Local<v8::Data> kind_data =
      iterator->GetInternalField(kIteratorSourceKindField);
  v8::Local<v8::Data> mode_data =
      iterator->GetInternalField(kIteratorModeField);
  if (!facade_data->IsValue() || !facade_data.As<v8::Value>()->IsObject() ||
      !index_data->IsValue() || !index_data.As<v8::Value>()->IsUint32() ||
      !kind_data->IsValue() || !kind_data.As<v8::Value>()->IsInt32() ||
      !mode_data->IsValue() || !mode_data.As<v8::Value>()->IsInt32()) {
    ThrowError(isolate, "Gossamer DOM iterator state is invalid");
    return;
  }
  v8::Local<v8::Object> facade =
      facade_data.As<v8::Value>().As<v8::Object>();
  uint32_t index = index_data.As<v8::Value>().As<v8::Uint32>()->Value();
  FacadeKind kind = static_cast<FacadeKind>(
      kind_data.As<v8::Value>().As<v8::Int32>()->Value());
  IteratorMode mode = static_cast<IteratorMode>(
      mode_data.As<v8::Value>().As<v8::Int32>()->Value());
  bool found = false;
  std::string error;
  v8::Local<v8::Value> value;
  if (!ReadFacadeValue(CurrentRealm(isolate), isolate->GetCurrentContext(),
                       facade, kind, index, &found, &error)
           .ToLocal(&value)) {
    ThrowError(isolate, error.empty() ? "reading DOM iterator failed" : error);
    return;
  }
  if (!found) {
    SetIteratorResult(isolate, v8::Undefined(isolate), true, info);
    return;
  }
  iterator->SetInternalField(kIteratorIndexField,
                             v8::Integer::NewFromUnsigned(isolate, index + 1));
  if (mode == IteratorMode::Keys) {
    value = v8::Integer::NewFromUnsigned(isolate, index);
  } else if (mode == IteratorMode::Entries) {
    v8::Local<v8::Array> entry = v8::Array::New(isolate, 2);
    v8::Local<v8::Context> context = isolate->GetCurrentContext();
    if (!entry
             ->Set(context, 0,
                   v8::Integer::NewFromUnsigned(isolate, index))
             .FromMaybe(false) ||
        !entry->Set(context, 1, value).FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to allocate a DOM iterator entry");
      return;
    }
    value = entry;
  }
  SetIteratorResult(isolate, value, false, info);
}

void FacadeIterator(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  if (info.This().IsEmpty() ||
      info.This()->InternalFieldCount() < kFacadeInternalFieldCount) {
    ThrowError(isolate, "DOM facade receiver is not a Gossamer facade");
    return;
  }
  int encoded = info.Data().As<v8::Int32>()->Value();
  FacadeKind kind = static_cast<FacadeKind>(encoded / 10);
  IteratorMode mode = static_cast<IteratorMode>(encoded % 10);
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  v8::Local<v8::Object> iterator;
  if (!realm->collection_iterator_template.Get(isolate)
           ->NewInstance(isolate->GetCurrentContext())
           .ToLocal(&iterator)) {
    ThrowError(isolate, "V8 failed to allocate a live DOM iterator");
    return;
  }
  iterator->SetInternalField(kIteratorFacadeField, info.This());
  iterator->SetInternalField(kIteratorIndexField,
                             v8::Integer::NewFromUnsigned(isolate, 0));
  iterator->SetInternalField(kIteratorSourceKindField,
                             v8::Integer::New(isolate, static_cast<int>(kind)));
  iterator->SetInternalField(kIteratorModeField,
                             v8::Integer::New(isolate, static_cast<int>(mode)));
  info.GetReturnValue().Set(iterator);
}

void FacadeForEach(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  if (info.This().IsEmpty() ||
      info.This()->InternalFieldCount() < kFacadeInternalFieldCount) {
    ThrowError(isolate, "DOM facade receiver is not a Gossamer facade");
    return;
  }
  if (info.Length() == 0 || !info[0]->IsFunction()) {
    ThrowError(isolate, "DOM facade forEach requires a callback");
    return;
  }
  v8::Local<v8::Function> callback = info[0].As<v8::Function>();
  v8::Local<v8::Value> receiver =
      info.Length() > 1 ? info[1] : v8::Undefined(isolate);
  FacadeKind kind = FacadeKindFromData(info.Data());
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  for (uint32_t index = 0;; ++index) {
    bool found = false;
    std::string error;
    v8::Local<v8::Value> value;
    if (!ReadFacadeValue(CurrentRealm(isolate), context, info.This(), kind,
                         index,
                         &found, &error)
             .ToLocal(&value)) {
      ThrowError(isolate, error.empty() ? "reading DOM facade failed" : error);
      return;
    }
    if (!found)
      return;
    v8::Local<v8::Value> arguments[] = {
        value, v8::Integer::NewFromUnsigned(isolate, index), info.This()};
    if (callback->Call(context, receiver, 3, arguments).IsEmpty())
      return;
    if (index == std::numeric_limits<uint32_t>::max())
      return;
  }
}

void ClassListValueGetter(v8::Local<v8::Name>,
                          const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.Holder(), &key))
    return;
  std::string rendered;
  bool found = false;
  std::string error;
  if (!ReadAttribute(CurrentRealm(isolate), key, "class", &rendered, &found,
                     &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::String> value;
  if (!NewUTF8String(isolate, rendered.data(), found ? rendered.size() : 0,
                     &value)) {
    ThrowError(isolate, "V8 failed to allocate classList.value");
    return;
  }
  info.GetReturnValue().Set(value);
}

void ClassListValueSetter(v8::Local<v8::Name>, v8::Local<v8::Value> value,
                          const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  std::string rendered;
  if (!ReadFacadeKey(isolate, info.Holder(), &key) ||
      !StringFromValue(isolate, value, &rendered)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string error;
  if (!WriteAttribute(CurrentRealm(isolate), key, "class", rendered, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  info.GetReturnValue().Set(true);
}

void ClassListToString(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.This(), &key))
    return;
  std::string rendered;
  bool found = false;
  std::string error;
  if (!ReadAttribute(CurrentRealm(isolate), key, "class", &rendered, &found,
                     &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::String> value;
  if (!NewUTF8String(isolate, rendered.data(), found ? rendered.size() : 0,
                     &value)) {
    ThrowError(isolate, "V8 failed to allocate classList string");
    return;
  }
  info.GetReturnValue().Set(value);
}

bool ReadClassTokenArgument(v8::Isolate *isolate, v8::Local<v8::Value> value,
                            std::string *token) {
  return StringFromValue(isolate, value, token) &&
         ValidateClassToken(isolate, *token);
}

void ClassListContains(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  std::string token;
  if (!ReadFacadeKey(isolate, info.This(), &key) ||
      !ReadClassTokenArgument(isolate,
                              info.Length() == 0 ? v8::Undefined(isolate)
                                                 : info[0],
                              &token))
    return;
  std::vector<std::string> tokens;
  std::string error;
  if (!ReadClassTokens(CurrentRealm(isolate), key, &tokens, &error)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(std::find(tokens.begin(), tokens.end(), token) !=
                            tokens.end());
}

void ClassListAdd(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.This(), &key))
    return;
  std::vector<std::string> additions;
  additions.reserve(info.Length());
  for (int index = 0; index < info.Length(); ++index) {
    std::string token;
    if (!ReadClassTokenArgument(isolate, info[index], &token))
      return;
    additions.push_back(std::move(token));
  }
  std::vector<std::string> tokens;
  std::string error;
  if (!ReadClassTokens(CurrentRealm(isolate), key, &tokens, &error)) {
    ThrowError(isolate, error);
    return;
  }
  for (const std::string &token : additions) {
    if (std::find(tokens.begin(), tokens.end(), token) == tokens.end())
      tokens.push_back(token);
  }
  if (!WriteClassTokens(CurrentRealm(isolate), key, tokens, &error))
    ThrowError(isolate, error);
}

void ClassListRemove(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.This(), &key))
    return;
  std::unordered_set<std::string> removals;
  for (int index = 0; index < info.Length(); ++index) {
    std::string token;
    if (!ReadClassTokenArgument(isolate, info[index], &token))
      return;
    removals.insert(std::move(token));
  }
  std::vector<std::string> tokens;
  std::string error;
  if (!ReadClassTokens(CurrentRealm(isolate), key, &tokens, &error)) {
    ThrowError(isolate, error);
    return;
  }
  tokens.erase(std::remove_if(tokens.begin(), tokens.end(),
                              [&removals](const std::string &token) {
                                return removals.contains(token);
                              }),
               tokens.end());
  if (!WriteClassTokens(CurrentRealm(isolate), key, tokens, &error))
    ThrowError(isolate, error);
}

void ClassListToggle(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  std::string token;
  if (!ReadFacadeKey(isolate, info.This(), &key) ||
      !ReadClassTokenArgument(isolate,
                              info.Length() == 0 ? v8::Undefined(isolate)
                                                 : info[0],
                              &token))
    return;
  std::vector<std::string> tokens;
  std::string error;
  if (!ReadClassTokens(CurrentRealm(isolate), key, &tokens, &error)) {
    ThrowError(isolate, error);
    return;
  }
  auto existing = std::find(tokens.begin(), tokens.end(), token);
  bool present = existing != tokens.end();
  bool desired = info.Length() > 1 ? info[1]->BooleanValue(isolate) : !present;
  if (desired && !present)
    tokens.push_back(token);
  else if (!desired && present)
    tokens.erase(existing);
  if (desired != present &&
      !WriteClassTokens(CurrentRealm(isolate), key, tokens, &error)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(desired);
}

void ClassListReplace(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  std::string from;
  std::string to;
  if (!ReadFacadeKey(isolate, info.This(), &key) ||
      !ReadClassTokenArgument(isolate,
                              info.Length() == 0 ? v8::Undefined(isolate)
                                                 : info[0],
                              &from) ||
      !ReadClassTokenArgument(isolate,
                              info.Length() < 2 ? v8::Undefined(isolate)
                                                : info[1],
                              &to))
    return;
  std::vector<std::string> tokens;
  std::string error;
  if (!ReadClassTokens(CurrentRealm(isolate), key, &tokens, &error)) {
    ThrowError(isolate, error);
    return;
  }
  auto existing = std::find(tokens.begin(), tokens.end(), from);
  if (existing == tokens.end()) {
    info.GetReturnValue().Set(false);
    return;
  }
  if (from != to) {
    if (std::find(tokens.begin(), tokens.end(), to) == tokens.end())
      *existing = to;
    else
      tokens.erase(existing);
    if (!WriteClassTokens(CurrentRealm(isolate), key, tokens, &error)) {
      ThrowError(isolate, error);
      return;
    }
  }
  info.GetReturnValue().Set(true);
}

v8::Intercepted DatasetNamedGetter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Value> &info) {
  if (!property->IsString())
    return v8::Intercepted::kNo;
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.Holder(), &key))
    return v8::Intercepted::kYes;
  std::string attribute;
  if (!DatasetAttributeFromName(UTF8Value(isolate, property.As<v8::Value>()),
                                &attribute))
    return v8::Intercepted::kNo;
  std::string value;
  bool found = false;
  std::string error;
  if (!ReadAttribute(CurrentRealm(isolate), key, attribute, &value, &found,
                     &error)) {
    ThrowError(isolate, error);
    return v8::Intercepted::kYes;
  }
  if (!found)
    return v8::Intercepted::kNo;
  v8::Local<v8::String> rendered;
  if (!NewUTF8String(isolate, value.data(), value.size(), &rendered)) {
    ThrowError(isolate, "V8 failed to allocate a dataset value");
    return v8::Intercepted::kYes;
  }
  info.GetReturnValue().Set(rendered);
  return v8::Intercepted::kYes;
}

v8::Intercepted DatasetNamedSetter(
    v8::Local<v8::Name> property, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  if (!property->IsString())
    return v8::Intercepted::kNo;
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.Holder(), &key)) {
    info.GetReturnValue().Set(false);
    return v8::Intercepted::kYes;
  }
  std::string attribute;
  std::string rendered;
  if (!DatasetAttributeFromName(UTF8Value(isolate, property.As<v8::Value>()),
                                &attribute)) {
    ThrowError(isolate, "dataset property names cannot contain '-' followed by a lowercase letter");
    info.GetReturnValue().Set(false);
    return v8::Intercepted::kYes;
  }
  if (!StringFromValue(isolate, value, &rendered)) {
    info.GetReturnValue().Set(false);
    return v8::Intercepted::kYes;
  }
  std::string error;
  if (!WriteAttribute(CurrentRealm(isolate), key, attribute, rendered,
                      &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return v8::Intercepted::kYes;
  }
  info.GetReturnValue().Set(true);
  return v8::Intercepted::kYes;
}

v8::Intercepted DatasetNamedQuery(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Integer> &info) {
  if (!property->IsString())
    return v8::Intercepted::kNo;
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.Holder(), &key))
    return v8::Intercepted::kYes;
  std::string attribute;
  if (!DatasetAttributeFromName(UTF8Value(isolate, property.As<v8::Value>()),
                                &attribute))
    return v8::Intercepted::kNo;
  std::string value;
  bool found = false;
  std::string error;
  if (!ReadAttribute(CurrentRealm(isolate), key, attribute, &value, &found,
                     &error)) {
    ThrowError(isolate, error);
    return v8::Intercepted::kYes;
  }
  if (!found)
    return v8::Intercepted::kNo;
  info.GetReturnValue().Set(v8::None);
  return v8::Intercepted::kYes;
}

v8::Intercepted DatasetNamedDeleter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  if (!property->IsString())
    return v8::Intercepted::kNo;
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.Holder(), &key)) {
    info.GetReturnValue().Set(false);
    return v8::Intercepted::kYes;
  }
  std::string attribute;
  if (!DatasetAttributeFromName(UTF8Value(isolate, property.As<v8::Value>()),
                                &attribute))
    return v8::Intercepted::kNo;
  std::string error;
  if (!DeleteAttribute(CurrentRealm(isolate), key, attribute, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return v8::Intercepted::kYes;
  }
  info.GetReturnValue().Set(true);
  return v8::Intercepted::kYes;
}

void DatasetNamedEnumerator(
    const v8::PropertyCallbackInfo<v8::Array> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadFacadeKey(isolate, info.Holder(), &key))
    return;
  std::vector<std::string> attributes;
  std::string error;
  if (!ReadAttributeNames(CurrentRealm(isolate), key, &attributes, &error)) {
    ThrowError(isolate, error);
    return;
  }
  std::vector<std::string> properties;
  std::unordered_set<std::string> seen;
  for (const std::string &attribute : attributes) {
    std::string property;
    if (DatasetNameFromAttribute(attribute, &property) &&
        seen.insert(property).second)
      properties.push_back(std::move(property));
  }
  if (properties.size() >
      static_cast<size_t>(std::numeric_limits<int>::max())) {
    ThrowError(isolate, "dataset exceeds V8's enumeration limit");
    return;
  }
  v8::Local<v8::Array> result =
      v8::Array::New(isolate, static_cast<int>(properties.size()));
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  for (size_t index = 0; index < properties.size(); ++index) {
    v8::Local<v8::String> property;
    if (!NewUTF8String(isolate, properties[index].data(),
                       properties[index].size(), &property) ||
        !result->Set(context, static_cast<uint32_t>(index), property)
             .FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to enumerate dataset properties");
      return;
    }
  }
  info.GetReturnValue().Set(result);
}

void NodeClassListGetter(v8::Local<v8::Name>,
                         const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  v8::Local<v8::Object> node = info.Holder();
  if (!ReadReceiverKey(isolate, node, &key))
    return;
  v8::Local<v8::Data> cached_data =
      node->GetInternalField(kNodeClassListField);
  if (cached_data->IsValue() && cached_data.As<v8::Value>()->IsObject()) {
    info.GetReturnValue().Set(cached_data.As<v8::Value>());
    return;
  }
  v8::Local<v8::Object> facade;
  if (!CurrentRealm(isolate)->token_list_template.Get(isolate)
           ->InstanceTemplate()
           ->NewInstance(isolate->GetCurrentContext())
           .ToLocal(&facade)) {
    ThrowError(isolate, "V8 failed to allocate classList");
    return;
  }
  facade->SetInternalField(kFacadeNodeField, node);
  node->SetInternalField(kNodeClassListField, facade);
  info.GetReturnValue().Set(facade);
}

void NodeDatasetGetter(v8::Local<v8::Name>,
                       const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  v8::Local<v8::Object> node = info.Holder();
  if (!ReadReceiverKey(isolate, node, &key))
    return;
  v8::Local<v8::Data> cached_data = node->GetInternalField(kNodeDatasetField);
  if (cached_data->IsValue() && cached_data.As<v8::Value>()->IsObject()) {
    info.GetReturnValue().Set(cached_data.As<v8::Value>());
    return;
  }
  v8::Local<v8::Object> facade;
  if (!CurrentRealm(isolate)->dataset_template.Get(isolate)
           ->InstanceTemplate()
           ->NewInstance(isolate->GetCurrentContext())
           .ToLocal(&facade)) {
    ThrowError(isolate, "V8 failed to allocate dataset");
    return;
  }
  facade->SetInternalField(kFacadeNodeField, node);
  node->SetInternalField(kNodeDatasetField, facade);
  info.GetReturnValue().Set(facade);
}

void NodeGetAttributeNames(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  std::vector<std::string> names;
  std::string error;
  if (!ReadAttributeNames(CurrentRealm(isolate), key, &names, &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (names.size() > static_cast<size_t>(std::numeric_limits<int>::max())) {
    ThrowError(isolate, "element exceeds V8's attribute enumeration limit");
    return;
  }
  v8::Local<v8::Array> result =
      v8::Array::New(isolate, static_cast<int>(names.size()));
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  for (size_t index = 0; index < names.size(); ++index) {
    v8::Local<v8::String> name;
    if (!NewUTF8String(isolate, names[index].data(), names[index].size(),
                       &name) ||
        !result->Set(context, static_cast<uint32_t>(index), name)
             .FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to allocate attribute names");
      return;
    }
  }
  info.GetReturnValue().Set(result);
}

bool ReadSelectorNodes(gossamer_v8_realm *realm, const WrapperKey &scope,
                       const std::string &selector, bool all,
                       std::vector<uint32_t> *nodes, std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  uint32_t *host_nodes = nullptr;
  size_t count = 0;
  char *host_error = nullptr;
  if (realm->active_host->query_selector(
          realm->active_host->execution_id, scope.document, scope.node,
          selector.data(), selector.size(), all ? 1 : 0, &host_nodes, &count,
          &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(host_nodes);
    if (error->empty())
      *error = "DOM selector query failed";
    return false;
  }
  std::free(host_error);
  if (count == 0)
    nodes->clear();
  else
    nodes->assign(host_nodes, host_nodes + count);
  std::free(host_nodes);
  return true;
}

v8::MaybeLocal<v8::Object>
CreateStaticNodeList(gossamer_v8_realm *realm,
                     v8::Local<v8::Context> context,
                     const WrapperKey &scope,
                     const std::vector<uint32_t> &nodes,
                     std::string *error) {
  v8::Isolate *isolate = realm->isolate;
  if (nodes.size() > static_cast<size_t>(std::numeric_limits<int>::max())) {
    *error = "selector result exceeds V8's collection limit";
    return {};
  }
  v8::Local<v8::Array> backing =
      v8::Array::New(isolate, static_cast<int>(nodes.size()));
  for (size_t index = 0; index < nodes.size(); ++index) {
    v8::Local<v8::Object> wrapper;
    if (!GetOrCreateNodeWrapper(realm, context,
                                WrapperKey{scope.document, nodes[index]}, error)
             .ToLocal(&wrapper) ||
        !backing->Set(context, static_cast<uint32_t>(index), wrapper)
             .FromMaybe(false)) {
      if (error->empty())
        *error = "V8 failed to populate a static NodeList";
      return {};
    }
  }
  v8::Local<v8::Object> facade;
  if (!realm->node_list_template.Get(isolate)
           ->InstanceTemplate()
           ->NewInstance(context)
           .ToLocal(&facade)) {
    *error = "V8 failed to allocate a static NodeList";
    return {};
  }
  facade->SetInternalField(kFacadeBackingField, backing);
  return facade;
}

void NodeQuerySelector(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey scope;
  if (!ReadReceiverKey(isolate, info.This(), &scope))
    return;
  std::string selector;
  if (!StringFromValue(isolate,
                       info.Length() == 0 ? v8::Undefined(isolate) : info[0],
                       &selector))
    return;
  bool all = !info.Data().IsEmpty() && info.Data()->IsTrue();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::vector<uint32_t> nodes;
  std::string error;
  if (!ReadSelectorNodes(realm, scope, selector, all, &nodes, &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  if (all) {
    v8::Local<v8::Object> list;
    if (!CreateStaticNodeList(realm, context, scope, nodes, &error)
             .ToLocal(&list)) {
      ThrowError(isolate, error);
      return;
    }
    info.GetReturnValue().Set(list);
    return;
  }
  if (nodes.empty()) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, context,
                              WrapperKey{scope.document, nodes.front()},
                              &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void ElementMatchesSelector(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  std::string selector;
  if (!StringFromValue(isolate,
                       info.Length() == 0 ? v8::Undefined(isolate) : info[0],
                       &selector))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  int matches = 0;
  char *host_error = nullptr;
  if (realm->active_host->matches_selector(
          realm->active_host->execution_id, key.document, key.node,
          selector.data(), selector.size(), &matches, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "DOM selector match failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(matches != 0);
}

void ElementClosestSelector(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  std::string selector;
  if (!StringFromValue(isolate,
                       info.Length() == 0 ? v8::Undefined(isolate) : info[0],
                       &selector))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  uint32_t closest = 0;
  int found = 0;
  char *host_error = nullptr;
  if (realm->active_host->closest_selector(
          realm->active_host->execution_id, key.document, key.node,
          selector.data(), selector.size(), &closest, &found,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "DOM closest selector failed" : error);
    return;
  }
  std::free(host_error);
  if (found == 0) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, isolate->GetCurrentContext(),
                              WrapperKey{key.document, closest}, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void ElementInnerHTMLGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *value = nullptr;
  size_t value_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->inner_html(
          realm->active_host->execution_id, key.document, key.node, &value,
          &value_length, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(value);
    ThrowError(isolate, error.empty() ? "reading innerHTML failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> result;
  bool allocated = NewUTF8String(isolate, value, value_length, &result);
  std::free(value);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate innerHTML");
    return;
  }
  info.GetReturnValue().Set(result);
}

void ElementInnerHTMLSetter(
    v8::Local<v8::Name>, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  std::string source;
  if (!ReadReceiverKey(isolate, info.Holder(), &key) ||
      !StringFromValue(isolate, value, &source)) {
    info.GetReturnValue().Set(false);
    return;
  }
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->set_inner_html(
          realm->active_host->execution_id, key.document, key.node,
          source.data(), source.size(), &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "setting innerHTML failed" : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

void ElementInsertAdjacentHTML(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  std::string position;
  std::string source;
  if (info.Length() < 2 || !StringFromValue(isolate, info[0], &position) ||
      !StringFromValue(isolate, info[1], &source)) {
    ThrowError(isolate, "insertAdjacentHTML requires a position and markup");
    return;
  }
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->insert_adjacent_html(
          realm->active_host->execution_id, key.document, key.node,
          position.data(), position.size(), source.data(), source.size(),
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "insertAdjacentHTML failed" : error);
    return;
  }
  std::free(host_error);
}

void ElementFormValueGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *value = nullptr;
  size_t value_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->form_value(
          realm->active_host->execution_id, key.document, key.node, &value,
          &value_length, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(value);
    ThrowError(isolate, error.empty() ? "reading form value failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> result;
  bool allocated = NewUTF8String(isolate, value, value_length, &result);
  std::free(value);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate a form value");
    return;
  }
  info.GetReturnValue().Set(result);
}

void ElementFormValueSetter(
    v8::Local<v8::Name>, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  std::string rendered;
  if (!ReadReceiverKey(isolate, info.Holder(), &key) ||
      !StringFromValue(isolate, value, &rendered)) {
    info.GetReturnValue().Set(false);
    return;
  }
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->set_form_value(
          realm->active_host->execution_id, key.document, key.node,
          rendered.data(), rendered.size(), &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "setting form value failed" : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

void ElementFormCheckedGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  int checked = 0;
  char *host_error = nullptr;
  if (realm->active_host->form_checked(
          realm->active_host->execution_id, key.document, key.node, &checked,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "reading form checked state failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(checked != 0);
}

void ElementFormCheckedSetter(
    v8::Local<v8::Name>, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key)) {
    info.GetReturnValue().Set(false);
    return;
  }
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->set_form_checked(
          realm->active_host->execution_id, key.document, key.node,
          value->BooleanValue(isolate) ? 1 : 0, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "setting form checked state failed" : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

void ElementFormValueFunctionGetter(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *value = nullptr;
  size_t value_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->form_value(
          realm->active_host->execution_id, key.document, key.node, &value,
          &value_length, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(value);
    ThrowError(isolate, error.empty() ? "reading form value failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> result;
  bool allocated = NewUTF8String(isolate, value, value_length, &result);
  std::free(value);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate a form value");
    return;
  }
  info.GetReturnValue().Set(result);
}

void ElementFormValueFunctionSetter(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  std::string rendered;
  if (!ReadReceiverKey(isolate, info.This(), &key) || info.Length() == 0 ||
      !StringFromValue(isolate, info[0], &rendered))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->set_form_value(
          realm->active_host->execution_id, key.document, key.node,
          rendered.data(), rendered.size(), &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "setting form value failed" : error);
    return;
  }
  std::free(host_error);
}

void ElementFormCheckedFunctionGetter(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  int checked = 0;
  char *host_error = nullptr;
  if (realm->active_host->form_checked(
          realm->active_host->execution_id, key.document, key.node, &checked,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "reading form checked state failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(checked != 0);
}

void ElementFormCheckedFunctionSetter(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key) || info.Length() == 0)
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->set_form_checked(
          realm->active_host->execution_id, key.document, key.node,
          info[0]->BooleanValue(isolate) ? 1 : 0, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "setting form checked state failed" : error);
    return;
  }
  std::free(host_error);
}

void HTMLElementFocus(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  bool focused = info.Data()->IsTrue();
  char *host_error = nullptr;
  if (realm->active_host->focus_node(
          realm->active_host->execution_id, key.document, key.node,
          focused ? 1 : 0, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "changing focus failed" : error);
    return;
  }
  std::free(host_error);
}

void DocumentActiveElementGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  uint64_t document = 0;
  uint32_t node = 0;
  int found = 0;
  char *host_error = nullptr;
  if (realm->active_host->active_element(
          realm->active_host->execution_id, &document, &node, &found,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "reading activeElement failed" : error);
    return;
  }
  std::free(host_error);
  if (found == 0) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, isolate->GetCurrentContext(),
                              WrapperKey{document, node}, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void NodeHasChildNodes(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  std::vector<uint32_t> nodes;
  std::string error;
  if (!ReadChildNodes(realm, key, false, &nodes, &error)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(v8::Boolean::New(isolate, !nodes.empty()));
}

void NodeContains(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  if (info.Length() == 0 || info[0]->IsNull()) {
    info.GetReturnValue().Set(false);
    return;
  }
  WrapperKey other;
  if (!ReadNodeArgument(isolate, info[0], &other,
                        "contains requires a Gossamer node or null"))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  int contains = 0;
  char *host_error = nullptr;
  if (realm->active_host->contains(
          realm->active_host->execution_id, key.document, key.node,
          other.document, other.node, &contains, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "contains failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(v8::Boolean::New(isolate, contains != 0));
}

void NodeReplaceChild(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey parent;
  WrapperKey child;
  WrapperKey replaced;
  if (!ReadReceiverKey(isolate, info.This(), &parent))
    return;
  if (info.Length() < 2 ||
      !ReadNodeArgument(isolate, info[0], &child,
                        "replaceChild requires a Gossamer child node") ||
      !ReadNodeArgument(isolate, info[1], &replaced,
                        "replaceChild requires a Gossamer replaced node"))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->replace_child(
          realm->active_host->execution_id, parent.document, parent.node,
          child.document, child.node, replaced.document, replaced.node,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "replaceChild failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(info[1]);
}

void NodeValueGetter(v8::Local<v8::Name>,
                     const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *value = nullptr;
  size_t value_length = 0;
  int non_null = 0;
  char *host_error = nullptr;
  if (realm->active_host->node_value(
          realm->active_host->execution_id, key.document, key.node, &value,
          &value_length, &non_null, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(value);
    ThrowError(isolate, error.empty() ? "reading nodeValue failed" : error);
    return;
  }
  std::free(host_error);
  if (non_null == 0) {
    std::free(value);
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  v8::Local<v8::String> result;
  bool allocated = NewUTF8String(isolate, value, value_length, &result);
  std::free(value);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate nodeValue");
    return;
  }
  info.GetReturnValue().Set(result);
}

void NodeValueSetter(v8::Local<v8::Name>, v8::Local<v8::Value> value,
                     const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string rendered;
  if (!StringFromValue(isolate, value, &rendered)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->set_node_value(
          realm->active_host->execution_id, key.document, key.node,
          rendered.data(), rendered.size(), &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "setting nodeValue failed" : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

void NodeReflectedAttributeGetter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  std::string property_name = UTF8Value(isolate, property.As<v8::Value>());
  std::string attribute = property_name;
  if (property_name == "className")
    attribute = "class";
  else if (property_name == "htmlFor")
    attribute = "for";
  else if (property_name == "defaultValue")
    attribute = "value";
  char *value = nullptr;
  size_t value_length = 0;
  int found = 0;
  char *host_error = nullptr;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (realm->active_host->get_attribute(
          realm->active_host->execution_id, key.document, key.node,
          attribute.data(),
          attribute.size(), &value, &value_length, &found,
          &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(value);
    ThrowError(isolate, error.empty() ? "reading reflected attribute failed"
                                      : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> result;
  bool allocated = NewUTF8String(isolate, value, found == 0 ? 0 : value_length,
                                 &result);
  std::free(value);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate a reflected attribute");
    return;
  }
  info.GetReturnValue().Set(result);
}

void NodeReflectedAttributeSetter(
    v8::Local<v8::Name> property, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string rendered;
  if (!StringFromValue(isolate, value, &rendered)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string property_name = UTF8Value(isolate, property.As<v8::Value>());
  std::string attribute = property_name;
  if (property_name == "className")
    attribute = "class";
  else if (property_name == "htmlFor")
    attribute = "for";
  else if (property_name == "defaultValue")
    attribute = "value";
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->set_attribute(
          realm->active_host->execution_id, key.document, key.node,
          attribute.data(),
          attribute.size(), rendered.data(), rendered.size(),
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "setting reflected attribute failed"
                                      : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

std::string ReflectedBooleanAttributeName(const std::string &property) {
  if (property == "defaultChecked")
    return "checked";
  return property;
}

void NodeReflectedBooleanGetter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  std::string attribute = ReflectedBooleanAttributeName(
      UTF8Value(isolate, property.As<v8::Value>()));
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  int found = 0;
  char *host_error = nullptr;
  if (realm->active_host->has_attribute(
          realm->active_host->execution_id, key.document, key.node,
          attribute.data(), attribute.size(), &found, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "reading reflected boolean failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(found != 0);
}

void NodeReflectedBooleanSetter(
    v8::Local<v8::Name> property, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string attribute = ReflectedBooleanAttributeName(
      UTF8Value(isolate, property.As<v8::Value>()));
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  char *host_error = nullptr;
  int ok = value->BooleanValue(isolate)
               ? realm->active_host->set_attribute(
                     realm->active_host->execution_id, key.document, key.node,
                     attribute.data(), attribute.size(), "", 0, &host_error)
               : realm->active_host->remove_attribute(
                     realm->active_host->execution_id, key.document, key.node,
                     attribute.data(), attribute.size(), &host_error);
  if (ok == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "setting reflected boolean failed" : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

void NodeHasAttribute(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  std::string name;
  if (info.Length() == 0 || !StringFromValue(isolate, info[0], &name))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  int found = 0;
  char *host_error = nullptr;
  if (realm->active_host->has_attribute(
          realm->active_host->execution_id, key.document, key.node, name.data(),
          name.size(), &found, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "hasAttribute failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(v8::Boolean::New(isolate, found != 0));
}

void NodeRemove(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  uint32_t parent_node = 0;
  int found = 0;
  char *host_error = nullptr;
  if (realm->active_host->related_node(
          realm->active_host->execution_id, key.document, key.node, 1,
          &parent_node, &found, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "remove traversal failed" : error);
    return;
  }
  std::free(host_error);
  if (found == 0)
    return;
  host_error = nullptr;
  if (realm->active_host->remove_child(
          realm->active_host->execution_id, key.document, parent_node,
          key.document, key.node, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "remove failed" : error);
    return;
  }
  std::free(host_error);
}

void NodeStyleGetter(v8::Local<v8::Name>,
                     const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  v8::Local<v8::Object> node = info.Holder();
  if (!ReadReceiverKey(isolate, node, &key))
    return;
  NodeMetadata metadata;
  std::string error;
  if (!ReadNodeMetadata(realm, key, &metadata, &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (metadata.type != 1) {
    info.GetReturnValue().Set(v8::Undefined(isolate));
    return;
  }
  v8::Local<v8::Data> cached_data = node->GetInternalField(kNodeStyleField);
  if (cached_data->IsValue()) {
    v8::Local<v8::Value> cached = cached_data.As<v8::Value>();
    if (cached->IsObject()) {
      info.GetReturnValue().Set(cached);
      return;
    }
  }
  v8::Local<v8::ObjectTemplate> style_template =
      realm->style_template.Get(isolate);
  v8::Local<v8::Object> style;
  if (!style_template->NewInstance(isolate->GetCurrentContext()).ToLocal(
          &style)) {
    ThrowError(isolate, "V8 failed to allocate element.style");
    return;
  }
  style->SetInternalField(kStyleNodeField, node);
  node->SetInternalField(kNodeStyleField, style);
  info.GetReturnValue().Set(style);
}

void StyleCSSTextGetter(v8::Local<v8::Name>,
                        const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadStyleKey(isolate, info.Holder(), &key))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *value = nullptr;
  size_t value_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->style_css_text(
          realm->active_host->execution_id, key.document, key.node, &value,
          &value_length, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(value);
    ThrowError(isolate, error.empty() ? "reading cssText failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> result;
  bool allocated = NewUTF8String(isolate, value, value_length, &result);
  std::free(value);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate cssText");
    return;
  }
  info.GetReturnValue().Set(result);
}

void StyleCSSTextSetter(v8::Local<v8::Name>, v8::Local<v8::Value> value,
                        const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadStyleKey(isolate, info.Holder(), &key)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string rendered;
  if (!StringFromValue(isolate, value, &rendered)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->set_style_css_text(
          realm->active_host->execution_id, key.document, key.node,
          rendered.data(), rendered.size(), &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "setting cssText failed" : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

bool ReadStyleProperty(gossamer_v8_realm *realm, const WrapperKey &key,
                       const std::string &name, std::string *value,
                       std::string *priority, bool *found,
                       std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_value = nullptr;
  size_t value_length = 0;
  char *host_priority = nullptr;
  size_t priority_length = 0;
  int host_found = 0;
  char *host_error = nullptr;
  if (realm->active_host->style_property(
          realm->active_host->execution_id, key.document, key.node, name.data(),
          name.size(), &host_value, &value_length, &host_priority,
          &priority_length, &host_found, &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(host_value);
    std::free(host_priority);
    if (error->empty())
      *error = "reading style property failed";
    return false;
  }
  std::free(host_error);
  value->assign(host_value == nullptr ? "" : host_value, value_length);
  priority->assign(host_priority == nullptr ? "" : host_priority,
                   priority_length);
  *found = host_found != 0;
  std::free(host_value);
  std::free(host_priority);
  return true;
}

bool SetStyleProperty(gossamer_v8_realm *realm, const WrapperKey &key,
                      const std::string &name, const std::string &value,
                      const std::string &priority, std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->set_style_property(
          realm->active_host->execution_id, key.document, key.node, name.data(),
          name.size(), value.data(), value.size(), priority.data(),
          priority.size(), &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "setting style property failed";
    return false;
  }
  std::free(host_error);
  return true;
}

void StyleLengthGetter(v8::Local<v8::Name>,
                       const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadStyleKey(isolate, info.Holder(), &key))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  size_t count = 0;
  char *host_error = nullptr;
  if (realm->active_host->style_property_count(
          realm->active_host->execution_id, key.document, key.node, &count,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "reading style length failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(
      v8::Number::New(isolate, static_cast<double>(count)));
}

void StyleItem(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadStyleKey(isolate, info.This(), &key))
    return;
  uint64_t index = 0;
  if (info.Length() != 0) {
    v8::Maybe<uint32_t> converted =
        info[0]->Uint32Value(isolate->GetCurrentContext());
    if (converted.IsNothing())
      return;
    index = converted.FromJust();
  }
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *name = nullptr;
  size_t name_length = 0;
  int found = 0;
  char *host_error = nullptr;
  if (realm->active_host->style_property_name(
          realm->active_host->execution_id, key.document, key.node,
          static_cast<size_t>(index), &name, &name_length, &found,
          &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(name);
    ThrowError(isolate, error.empty() ? "reading style item failed" : error);
    return;
  }
  std::free(host_error);
  if (found == 0) {
    std::free(name);
    v8::Local<v8::String> empty = v8::String::Empty(isolate);
    info.GetReturnValue().Set(empty);
    return;
  }
  v8::Local<v8::String> result;
  bool allocated = NewUTF8String(isolate, name, name_length, &result);
  std::free(name);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate style item");
    return;
  }
  info.GetReturnValue().Set(result);
}

void StyleGetPropertyValue(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadStyleKey(isolate, info.This(), &key))
    return;
  std::string name;
  if (info.Length() == 0 || !StringFromValue(isolate, info[0], &name))
    return;
  std::string value;
  std::string priority;
  std::string error;
  bool found = false;
  if (!ReadStyleProperty(realm, key, name, &value, &priority, &found, &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::String> result;
  if (!NewUTF8String(isolate, value.data(), found ? value.size() : 0,
                     &result)) {
    ThrowError(isolate, "V8 failed to allocate a style property value");
    return;
  }
  info.GetReturnValue().Set(result);
}

void StyleGetPropertyPriority(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadStyleKey(isolate, info.This(), &key))
    return;
  std::string name;
  if (info.Length() == 0 || !StringFromValue(isolate, info[0], &name))
    return;
  std::string value;
  std::string priority;
  std::string error;
  bool found = false;
  if (!ReadStyleProperty(realm, key, name, &value, &priority, &found, &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::String> result;
  if (!NewUTF8String(isolate, priority.data(), found ? priority.size() : 0,
                     &result)) {
    ThrowError(isolate, "V8 failed to allocate a style priority");
    return;
  }
  info.GetReturnValue().Set(result);
}

void StyleSetProperty(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadStyleKey(isolate, info.This(), &key))
    return;
  std::string name;
  std::string value;
  std::string priority;
  if (info.Length() < 2 || !StringFromValue(isolate, info[0], &name) ||
      !StringFromValue(isolate, info[1], &value) ||
      (info.Length() > 2 &&
       !StringFromValue(isolate, info[2], &priority)))
    return;
  std::string error;
  if (!SetStyleProperty(realm, key, name, value, priority, &error))
    ThrowError(isolate, error);
}

void StyleRemoveProperty(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadStyleKey(isolate, info.This(), &key))
    return;
  std::string name;
  if (info.Length() == 0 || !StringFromValue(isolate, info[0], &name))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *value = nullptr;
  size_t value_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->remove_style_property(
          realm->active_host->execution_id, key.document, key.node, name.data(),
          name.size(), &value, &value_length, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(value);
    ThrowError(isolate, error.empty() ? "removeProperty failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> result;
  bool allocated = NewUTF8String(isolate, value, value_length, &result);
  std::free(value);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate the removed style value");
    return;
  }
  info.GetReturnValue().Set(result);
}

void StyleDirectPropertyGetter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadStyleKey(isolate, info.Holder(), &key))
    return;
  std::string name =
      CSSPropertyNameFromJS(UTF8Value(isolate, property.As<v8::Value>()));
  std::string value;
  std::string priority;
  std::string error;
  bool found = false;
  if (!ReadStyleProperty(realm, key, name, &value, &priority, &found, &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::String> result;
  if (!NewUTF8String(isolate, value.data(), found ? value.size() : 0,
                     &result)) {
    ThrowError(isolate, "V8 failed to allocate a direct style property");
    return;
  }
  info.GetReturnValue().Set(result);
}

void StyleDirectPropertySetter(
    v8::Local<v8::Name> property, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadStyleKey(isolate, info.Holder(), &key)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string name =
      CSSPropertyNameFromJS(UTF8Value(isolate, property.As<v8::Value>()));
  std::string rendered;
  if (!StringFromValue(isolate, value, &rendered)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string error;
  if (!SetStyleProperty(realm, key, name, rendered, "", &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  info.GetReturnValue().Set(true);
}

bool ReadBooleanOption(v8::Local<v8::Context> context,
                       v8::Local<v8::Object> options, const char *name,
                       bool fallback, bool *value) {
  v8::Local<v8::Value> property;
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  if (!options
           ->Get(context,
                 v8::String::NewFromUtf8(isolate, name)
                     .ToLocalChecked())
           .ToLocal(&property))
    return false;
  *value = property->IsUndefined() ? fallback : property->BooleanValue(isolate);
  return true;
}

bool ReadStringOption(v8::Local<v8::Context> context,
                      v8::Local<v8::Object> options, const char *name,
                      std::string *value) {
  v8::Local<v8::Value> property;
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  if (!options
           ->Get(context,
                 v8::String::NewFromUtf8(isolate, name)
                     .ToLocalChecked())
           .ToLocal(&property))
    return false;
  if (property->IsUndefined() || property->IsNull()) {
    value->clear();
    return true;
  }
  return StringFromValue(isolate, property, value);
}

bool ReadNumberOption(v8::Local<v8::Context> context,
                      v8::Local<v8::Object> options, const char *name,
                      double fallback, double *value) {
  v8::Local<v8::Value> property;
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  if (!options
           ->Get(context,
                 v8::String::NewFromUtf8(isolate, name)
                     .ToLocalChecked())
           .ToLocal(&property))
    return false;
  if (property->IsUndefined()) {
    *value = fallback;
    return true;
  }
  return property->NumberValue(context).To(value);
}

bool ReadListenerOptions(v8::Local<v8::Context> context,
                         v8::Local<v8::Value> value, bool *capture,
                         bool *once, bool *passive) {
  *capture = false;
  *once = false;
  *passive = false;
  if (value.IsEmpty() || value->IsUndefined() || value->IsNull())
    return true;
  if (value->IsBoolean()) {
    *capture = value->BooleanValue(v8::Isolate::GetCurrent());
    return true;
  }
  if (!value->IsObject())
    return true;
  v8::Local<v8::Object> options = value.As<v8::Object>();
  return ReadBooleanOption(context, options, "capture", false, capture) &&
         ReadBooleanOption(context, options, "once", false, once) &&
         ReadBooleanOption(context, options, "passive", false, passive);
}

EventState *ReadEventState(v8::Isolate *isolate,
                           v8::Local<v8::Object> receiver) {
  if (receiver.IsEmpty() || receiver->InternalFieldCount() != 1) {
    ThrowError(isolate, "Event method receiver is not a Gossamer Event");
    return nullptr;
  }
  v8::Local<v8::Data> data = receiver->GetInternalField(kEventStateField);
  if (!data->IsValue() || !data.As<v8::Value>()->IsExternal()) {
    ThrowError(isolate, "Gossamer Event lost its native state");
    return nullptr;
  }
  return static_cast<EventState *>(
      data.As<v8::Value>().As<v8::External>()->Value(
          v8::kExternalPointerTypeTagDefault));
}

void EventCollected(const v8::WeakCallbackInfo<EventWeakData> &info) {
  EventWeakData *weak = info.GetParameter();
  if (weak == nullptr)
    return;
  if (weak->realm != nullptr)
    weak->realm->events.erase(weak);
  weak->object.Reset();
  delete weak->state;
  delete weak;
}

bool TrackEventObject(gossamer_v8_realm *realm, v8::Local<v8::Object> object,
                      EventState *state) {
  std::unique_ptr<EventWeakData> weak(new EventWeakData{realm, state, {}});
  object->SetInternalField(kEventStateField,
                           v8::External::New(
                               realm->isolate, state,
                               v8::kExternalPointerTypeTagDefault));
  weak->object.Reset(realm->isolate, object);
  weak->object.SetWeak(weak.get(), EventCollected,
                       v8::WeakCallbackType::kParameter);
  realm->events.insert(weak.get());
  weak.release();
  return true;
}

bool ParseEventOptions(v8::Local<v8::Context> context,
                       v8::Local<v8::Value> value, EventState *state) {
  if (value.IsEmpty() || value->IsUndefined() || value->IsNull())
    return true;
  if (!value->IsObject())
    return true;
  v8::Local<v8::Object> options = value.As<v8::Object>();
  double number = 0;
  if (!ReadBooleanOption(context, options, "bubbles", false,
                         &state->bubbles) ||
      !ReadBooleanOption(context, options, "cancelable", false,
                         &state->cancelable) ||
      !ReadBooleanOption(context, options, "composed", false,
                         &state->composed) ||
      !ReadNumberOption(context, options, "clientX", 0, &state->client_x) ||
      !ReadNumberOption(context, options, "clientY", 0, &state->client_y) ||
      !ReadNumberOption(context, options, "button", 0, &number))
    return false;
  state->button = static_cast<int32_t>(number);
  if (!ReadNumberOption(context, options, "buttons", 0, &number))
    return false;
  state->buttons = static_cast<uint32_t>(number);
  if (!ReadNumberOption(context, options, "pointerId", 0, &number))
    return false;
  state->pointer_id = static_cast<int32_t>(number);
  return ReadStringOption(context, options, "pointerType",
                          &state->pointer_type) &&
         ReadStringOption(context, options, "key", &state->key) &&
         ReadStringOption(context, options, "code", &state->code) &&
         ReadStringOption(context, options, "data", &state->data) &&
         ReadStringOption(context, options, "inputType", &state->input_type) &&
         ReadBooleanOption(context, options, "isPrimary", false,
                           &state->is_primary) &&
         ReadBooleanOption(context, options, "repeat", false,
                           &state->repeat) &&
         ReadBooleanOption(context, options, "isComposing", false,
                           &state->is_composing) &&
         ReadBooleanOption(context, options, "altKey", false,
                           &state->alt_key) &&
         ReadBooleanOption(context, options, "ctrlKey", false,
                           &state->ctrl_key) &&
         ReadBooleanOption(context, options, "metaKey", false,
                           &state->meta_key) &&
         ReadBooleanOption(context, options, "shiftKey", false,
                           &state->shift_key);
}

void EventConstructor(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  if (!info.IsConstructCall()) {
    ThrowError(isolate, "Event constructor must be called with new");
    return;
  }
  if (info.Length() == 0 || !info[0]->IsString()) {
    ThrowError(isolate, "Event constructor requires a type");
    return;
  }
  std::string type = UTF8Value(isolate, info[0]);
  auto state = std::make_unique<EventState>();
  state->type = std::move(type);
  state->timestamp = static_cast<double>(MonotonicNanos()) / 1000000.0;
  if (!info.Data().IsEmpty() && info.Data()->IsInt32()) {
    state->interface = static_cast<EventInterface>(
        info.Data().As<v8::Int32>()->Value());
  }
  if (!ParseEventOptions(isolate->GetCurrentContext(),
                         info.Length() > 1 ? info[1]
                                           : v8::Undefined(isolate),
                         state.get()))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  TrackEventObject(realm, info.This(), state.release());
  info.GetReturnValue().Set(info.This());
}

void EventPropertyGetter(v8::Local<v8::Name> property,
                         const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  EventState *state = ReadEventState(isolate, info.Holder());
  if (state == nullptr)
    return;
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  if (name == "type") {
    info.GetReturnValue().Set(
        v8::String::NewFromUtf8(isolate, state->type.data(),
                                v8::NewStringType::kNormal,
                                static_cast<int>(state->type.size()))
            .ToLocalChecked());
  } else if (name == "bubbles") {
    info.GetReturnValue().Set(state->bubbles);
  } else if (name == "cancelable") {
    info.GetReturnValue().Set(state->cancelable);
  } else if (name == "composed") {
    info.GetReturnValue().Set(state->composed);
  } else if (name == "defaultPrevented") {
    info.GetReturnValue().Set(state->default_prevented);
  } else if (name == "eventPhase") {
    info.GetReturnValue().Set(state->phase);
  } else if (name == "isTrusted") {
    info.GetReturnValue().Set(state->trusted);
  } else if (name == "timeStamp") {
    info.GetReturnValue().Set(state->timestamp);
  } else if (name == "cancelBubble") {
    info.GetReturnValue().Set(state->propagation_stopped);
  } else if (name == "returnValue") {
    info.GetReturnValue().Set(!state->default_prevented);
  } else if (name == "clientX") {
    info.GetReturnValue().Set(state->client_x);
  } else if (name == "clientY") {
    info.GetReturnValue().Set(state->client_y);
  } else if (name == "button") {
    info.GetReturnValue().Set(state->button);
  } else if (name == "buttons") {
    info.GetReturnValue().Set(state->buttons);
  } else if (name == "pointerId") {
    info.GetReturnValue().Set(state->pointer_id);
  } else if (name == "isPrimary") {
    info.GetReturnValue().Set(state->is_primary);
  } else if (name == "repeat") {
    info.GetReturnValue().Set(state->repeat);
  } else if (name == "isComposing") {
    info.GetReturnValue().Set(state->is_composing);
  } else if (name == "altKey") {
    info.GetReturnValue().Set(state->alt_key);
  } else if (name == "ctrlKey") {
    info.GetReturnValue().Set(state->ctrl_key);
  } else if (name == "metaKey") {
    info.GetReturnValue().Set(state->meta_key);
  } else if (name == "shiftKey") {
    info.GetReturnValue().Set(state->shift_key);
  } else if (name == "pointerType" || name == "key" || name == "code" ||
             name == "data" || name == "inputType") {
    const std::string *value = &state->pointer_type;
    if (name == "key")
      value = &state->key;
    else if (name == "code")
      value = &state->code;
    else if (name == "data")
      value = &state->data;
    else if (name == "inputType")
      value = &state->input_type;
    info.GetReturnValue().Set(
        v8::String::NewFromUtf8(isolate, value->data(),
                                v8::NewStringType::kNormal,
                                static_cast<int>(value->size()))
            .ToLocalChecked());
  } else if (name == "target" || name == "currentTarget" ||
             name == "relatedTarget") {
    bool present = state->has_target;
    WrapperKey key = state->target;
    if (name == "currentTarget") {
      present = state->has_current_target;
      key = state->current_target;
    } else if (name == "relatedTarget") {
      present = state->has_related_target;
      key = state->related_target;
    }
    if (!present) {
      info.GetReturnValue().Set(v8::Null(isolate));
      return;
    }
    gossamer_v8_realm *realm = CurrentRealm(isolate);
    std::string error;
    v8::Local<v8::Object> wrapper;
    if (!GetOrCreateNodeWrapper(realm, isolate->GetCurrentContext(), key,
                                &error)
             .ToLocal(&wrapper)) {
      ThrowError(isolate, error.empty() ? "V8 failed to wrap an Event target"
                                        : error);
      return;
    }
    info.GetReturnValue().Set(wrapper);
  }
}

void EventPropertySetter(v8::Local<v8::Name> property,
                         v8::Local<v8::Value> value,
                         const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  EventState *state = ReadEventState(info.GetIsolate(), info.Holder());
  if (state == nullptr) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string name = UTF8Value(info.GetIsolate(), property.As<v8::Value>());
  bool rendered = value->BooleanValue(info.GetIsolate());
  if (name == "cancelBubble" && rendered)
    state->propagation_stopped = true;
  if (name == "returnValue" && !rendered && state->cancelable &&
      !state->in_passive_listener)
    state->default_prevented = true;
  info.GetReturnValue().Set(true);
}

void EventStopPropagation(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  EventState *state = ReadEventState(info.GetIsolate(), info.This());
  if (state != nullptr)
    state->propagation_stopped = true;
}

void EventStopImmediatePropagation(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  EventState *state = ReadEventState(info.GetIsolate(), info.This());
  if (state != nullptr) {
    state->propagation_stopped = true;
    state->immediate_stopped = true;
  }
}

void EventPreventDefault(const v8::FunctionCallbackInfo<v8::Value> &info) {
  EventState *state = ReadEventState(info.GetIsolate(), info.This());
  if (state != nullptr && state->cancelable && !state->in_passive_listener)
    state->default_prevented = true;
}

void EventComposedPath(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  EventState *state = ReadEventState(isolate, info.This());
  if (state == nullptr)
    return;
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Array> result = v8::Array::New(
      isolate, state->dispatching ? static_cast<int>(state->path.size()) : 0);
  if (state->dispatching) {
    gossamer_v8_realm *realm = CurrentRealm(isolate);
    for (size_t index = 0; index < state->path.size(); ++index) {
      std::string error;
      v8::Local<v8::Object> wrapper;
      if (!GetOrCreateNodeWrapper(realm, context, state->path[index], &error)
               .ToLocal(&wrapper) ||
          !result->Set(context, static_cast<uint32_t>(index), wrapper)
               .FromMaybe(false)) {
        ThrowError(isolate, error.empty() ? "V8 failed to build composedPath"
                                          : error);
        return;
      }
    }
  }
  info.GetReturnValue().Set(result);
}

bool RetainListenerTarget(gossamer_v8_realm *realm, const WrapperKey &key,
                          std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->retain_node_event_target(
          realm->active_host->execution_id, key.document, key.node,
          &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "Go host rejected the EventTarget lifetime";
    return false;
  }
  std::free(host_error);
  return true;
}

bool ReleaseListenerTarget(gossamer_v8_realm *realm, const WrapperKey &key,
                           std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->release_node_event_target(
          realm->active_host->execution_id, key.document, key.node,
          &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "Go host rejected the EventTarget release";
    return false;
  }
  std::free(host_error);
  return true;
}

bool RemoveListenerRecord(gossamer_v8_realm *realm, const ListenerKey &key,
                          ListenerRecord *listener, std::string *error) {
  if (listener->removed)
    return true;
  if (!ReleaseListenerTarget(realm, key.target, error))
    return false;
  listener->removed = true;
  --realm->event_listener_count;
  return true;
}

void CleanupRemovedListeners(gossamer_v8_realm *realm) {
  if (realm->dispatch_depth != 0)
    return;
  for (auto entry = realm->listeners.begin(); entry != realm->listeners.end();) {
    auto &listeners = entry->second;
    listeners.erase(
        std::remove_if(listeners.begin(), listeners.end(),
                       [](const std::unique_ptr<ListenerRecord> &listener) {
                         if (!listener->removed)
                           return false;
                         listener->callback.Reset();
                         return true;
                       }),
        listeners.end());
    if (listeners.empty())
      entry = realm->listeners.erase(entry);
    else
      ++entry;
  }
}

void NodeAddEventListener(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  if (info.Length() < 2 || !info[1]->IsFunction()) {
    ThrowError(isolate, "addEventListener requires an event type and function");
    return;
  }
  std::string event_type;
  if (!EventTypeFromValue(isolate, info[0], &event_type))
    return;
  bool capture = false;
  bool once = false;
  bool passive = false;
  if (!ReadListenerOptions(isolate->GetCurrentContext(),
                           info.Length() > 2 ? info[2]
                                             : v8::Undefined(isolate),
                           &capture, &once, &passive))
    return;
  v8::Local<v8::Function> function = info[1].As<v8::Function>();
  ListenerKey listener_key{key, event_type};
  auto found = realm->listeners.find(listener_key);
  if (found != realm->listeners.end()) {
    for (const auto &listener : found->second) {
      if (!listener->removed && listener->capture == capture &&
          listener->callback == function)
        return;
    }
  }
  std::string error;
  if (!RetainListenerTarget(realm, key, &error)) {
    ThrowError(isolate, error);
    return;
  }
  auto listener = std::make_unique<ListenerRecord>();
  listener->id = realm->next_listener++;
  listener->callback.Reset(isolate, function);
  listener->capture = capture;
  listener->once = once;
  listener->passive = passive;
  realm->listeners[listener_key].push_back(std::move(listener));
  ++realm->event_listener_count;
}

void NodeRemoveEventListener(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  if (info.Length() < 2 || !info[1]->IsFunction())
    return;
  std::string event_type;
  if (!EventTypeFromValue(isolate, info[0], &event_type))
    return;
  bool capture = false;
  bool once = false;
  bool passive = false;
  if (!ReadListenerOptions(isolate->GetCurrentContext(),
                           info.Length() > 2 ? info[2]
                                             : v8::Undefined(isolate),
                           &capture, &once, &passive))
    return;
  ListenerKey listener_key{key, event_type};
  auto found = realm->listeners.find(listener_key);
  if (found == realm->listeners.end())
    return;
  v8::Local<v8::Function> function = info[1].As<v8::Function>();
  for (const auto &listener : found->second) {
    if (!listener->removed && listener->capture == capture &&
        listener->callback == function) {
      std::string error;
      if (!RemoveListenerRecord(realm, listener_key, listener.get(), &error)) {
        ThrowError(isolate, error);
        return;
      }
      break;
    }
  }
  CleanupRemovedListeners(realm);
}

bool ReadEventParent(gossamer_v8_realm *realm, const WrapperKey &key,
                     WrapperKey *parent, bool *found, std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  uint32_t related_node = 0;
  int related_found = 0;
  char *host_error = nullptr;
  if (realm->active_host->related_node(
          realm->active_host->execution_id, key.document, key.node, 1,
          &related_node, &related_found, &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "reading the EventTarget parent failed";
    return false;
  }
  std::free(host_error);
  *found = related_found != 0;
  *parent = WrapperKey{key.document, related_node};
  return true;
}

bool BuildEventPath(gossamer_v8_realm *realm, const WrapperKey &target,
                    std::vector<WrapperKey> *path, std::string *error) {
  path->clear();
  std::unordered_set<WrapperKey, WrapperKeyHash> seen;
  WrapperKey current = target;
  while (true) {
    if (!seen.insert(current).second) {
      *error = "DOM parent cycle encountered during event dispatch";
      return false;
    }
    path->push_back(current);
    WrapperKey parent;
    bool found = false;
    if (!ReadEventParent(realm, current, &parent, &found, error))
      return false;
    if (!found)
      return true;
    current = parent;
  }
}

bool InvokeEventListeners(gossamer_v8_realm *realm,
                          v8::Local<v8::Context> context,
                          v8::Local<v8::Object> event_object,
                          EventState *state, const WrapperKey &target,
                          uint8_t phase, bool capture, uint64_t maximum_id,
                          std::string *error) {
  ListenerKey key{target, state->type};
  auto found = realm->listeners.find(key);
  if (found == realm->listeners.end())
    return true;
  std::vector<ListenerRecord *> snapshot;
  snapshot.reserve(found->second.size());
  for (const auto &listener : found->second) {
    if (!listener->removed && listener->capture == capture &&
        listener->id <= maximum_id)
      snapshot.push_back(listener.get());
  }
  if (snapshot.empty())
    return true;

  state->phase = phase;
  state->current_target = target;
  state->has_current_target = true;
  std::string wrapper_error;
  v8::Local<v8::Object> current_target;
  if (!GetOrCreateNodeWrapper(realm, context, target, &wrapper_error)
           .ToLocal(&current_target)) {
    *error = wrapper_error.empty() ? "V8 failed to wrap currentTarget"
                                   : wrapper_error;
    return false;
  }
  v8::Local<v8::Value> arguments[] = {event_object};
  for (ListenerRecord *listener : snapshot) {
    if (listener->removed)
      continue;
    if (listener->once &&
        !RemoveListenerRecord(realm, key, listener, error))
      return false;
    state->in_passive_listener = listener->passive;
    v8::TryCatch caught(realm->isolate);
    v8::Local<v8::Value> result;
    bool called = listener->callback.Get(realm->isolate)
                      ->Call(context, current_target, 1, arguments)
                      .ToLocal(&result);
    state->in_passive_listener = false;
    if (!called) {
      *error = DescribeException(realm->isolate, context, caught);
      return false;
    }
    if (state->immediate_stopped)
      break;
  }
  return true;
}

void FinishEventDispatch(gossamer_v8_realm *realm, EventState *state) {
  state->phase = kEventPhaseNone;
  state->has_current_target = false;
  state->current_target = WrapperKey{};
  state->path.clear();
  state->dispatching = false;
  state->in_passive_listener = false;
  state->immediate_stopped = false;
  if (realm->dispatch_depth != 0)
    --realm->dispatch_depth;
  CleanupRemovedListeners(realm);
}

bool DispatchEventState(gossamer_v8_realm *realm,
                        v8::Local<v8::Context> context,
                        const WrapperKey &target,
                        v8::Local<v8::Object> event_object,
                        EventState *state, std::string *error) {
  if (state->dispatching) {
    *error = "Event is already being dispatched";
    return false;
  }
  if (!BuildEventPath(realm, target, &state->path, error))
    return false;
  state->target = target;
  state->has_target = true;
  state->propagation_stopped = false;
  state->immediate_stopped = false;
  state->dispatching = true;
  ++realm->dispatch_depth;
  realm->events_dispatched.fetch_add(1, std::memory_order_relaxed);
  uint64_t maximum_id = realm->next_listener - 1;
  bool ok = true;

  for (size_t index = state->path.size(); index > 1; --index) {
    state->immediate_stopped = false;
    if (!InvokeEventListeners(realm, context, event_object, state,
                              state->path[index - 1], kEventPhaseCapturing,
                              true, maximum_id, error)) {
      ok = false;
      break;
    }
    if (state->propagation_stopped)
      break;
  }

  if (ok && !state->propagation_stopped) {
    state->immediate_stopped = false;
    ok = InvokeEventListeners(realm, context, event_object, state, target,
                              kEventPhaseAtTarget, true, maximum_id, error);
    if (ok && !state->immediate_stopped) {
      ok = InvokeEventListeners(realm, context, event_object, state, target,
                                kEventPhaseAtTarget, false, maximum_id, error);
    }
  }

  if (ok && state->bubbles && !state->propagation_stopped) {
    for (size_t index = 1; index < state->path.size(); ++index) {
      state->immediate_stopped = false;
      if (!InvokeEventListeners(realm, context, event_object, state,
                                state->path[index], kEventPhaseBubbling, false,
                                maximum_id, error)) {
        ok = false;
        break;
      }
      if (state->propagation_stopped)
        break;
    }
  }

  FinishEventDispatch(realm, state);
  return ok;
}

void EventTargetDispatchEvent(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey target;
  if (!ReadReceiverKey(isolate, info.This(), &target))
    return;
  if (info.Length() == 0 || !info[0]->IsObject()) {
    ThrowError(isolate, "dispatchEvent requires an Event");
    return;
  }
  v8::Local<v8::Object> event_object = info[0].As<v8::Object>();
  EventState *state = ReadEventState(isolate, event_object);
  if (state == nullptr)
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!DispatchEventState(realm, isolate->GetCurrentContext(), target,
                          event_object, state, &error)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(!state->default_prevented);
}

v8::Local<v8::FunctionTemplate>
EventTemplateForInterface(gossamer_v8_realm *realm,
                          EventInterface interface) {
  switch (interface) {
  case EventInterface::MouseEvent:
    return realm->mouse_event_template.Get(realm->isolate);
  case EventInterface::PointerEvent:
    return realm->pointer_event_template.Get(realm->isolate);
  case EventInterface::KeyboardEvent:
    return realm->keyboard_event_template.Get(realm->isolate);
  case EventInterface::InputEvent:
    return realm->input_event_template.Get(realm->isolate);
  case EventInterface::FocusEvent:
    return realm->focus_event_template.Get(realm->isolate);
  case EventInterface::Event:
  default:
    return realm->event_template.Get(realm->isolate);
  }
}

v8::MaybeLocal<v8::Object>
NewEventObject(gossamer_v8_realm *realm, v8::Local<v8::Context> context,
               std::unique_ptr<EventState> state) {
  v8::Local<v8::Object> object;
  if (!EventTemplateForInterface(realm, state->interface)
           ->InstanceTemplate()
           ->NewInstance(context)
           .ToLocal(&object))
    return {};
  TrackEventObject(realm, object, state.release());
  return object;
}
void QueueMicrotaskCallback(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (info.Length() == 0 || !info[0]->IsFunction()) {
    ThrowError(isolate, "queueMicrotask requires a function");
    return;
  }
  uint64_t callback = StoreOneShotCallback(realm, info[0].As<v8::Function>());
  std::string error;
  if (!QueuePublishedCallback(realm, callback, true, &error)) {
    RemoveCallback(realm, callback);
    ThrowError(isolate, error);
  }
}

void SetTimeoutCallback(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (info.Length() == 0 || !info[0]->IsFunction()) {
    ThrowError(isolate, "setTimeout requires a function");
    return;
  }
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  double requested_delay = 0;
  if (info.Length() > 1 && !info[1]
                                ->NumberValue(isolate->GetCurrentContext())
                                .To(&requested_delay)) {
    return;
  }
  if (!std::isfinite(requested_delay) || requested_delay < 0)
    requested_delay = 0;
  int64_t delay = static_cast<int64_t>(
      std::min(std::floor(requested_delay),
               static_cast<double>(kMaximumDelayMilliseconds)));
  uint64_t callback = StoreOneShotCallback(realm, info[0].As<v8::Function>());
  uint64_t timer = 0;
  char *host_error = nullptr;
  if (realm->active_host->set_timeout(realm->active_host->execution_id,
                                      callback, delay, &timer,
                                      &host_error) == 0) {
    error = TakeCString(host_error);
    RemoveCallback(realm, callback);
    ThrowError(isolate, error.empty() ? "setTimeout failed" : error);
    return;
  }
  std::free(host_error);
  realm->timer_callbacks[timer] = callback;
  realm->callback_timers[callback] = timer;
  info.GetReturnValue().Set(
      v8::Number::New(isolate, static_cast<double>(timer)));
}

bool TimerIDFromValue(v8::Local<v8::Context> context,
                      v8::Local<v8::Value> value, uint64_t *timer) {
  if (value->IsBigInt()) {
    bool lossless = false;
    *timer = value.As<v8::BigInt>()->Uint64Value(&lossless);
    return lossless;
  }
  double number = 0;
  if (!value->NumberValue(context).To(&number) || !std::isfinite(number) ||
      number < 0 ||
      number > static_cast<double>(std::numeric_limits<uint64_t>::max())) {
    return false;
  }
  *timer = static_cast<uint64_t>(number);
  return true;
}

void ClearTimeoutCallback(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (info.Length() == 0)
    return;
  uint64_t timer = 0;
  if (!TimerIDFromValue(isolate->GetCurrentContext(), info[0], &timer))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->clear_timeout(realm->active_host->execution_id, timer,
                                        &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "clearTimeout failed" : error);
    return;
  }
  std::free(host_error);
  auto callback = realm->timer_callbacks.find(timer);
  if (callback != realm->timer_callbacks.end())
    RemoveCallback(realm, callback->second);
}

void IllegalDOMConstructor(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  ThrowError(info.GetIsolate(), "Illegal constructor");
}

bool InstallBindings(gossamer_v8_realm *realm, v8::Local<v8::Context> context) {
  v8::Isolate *isolate = realm->isolate;
  v8::Local<v8::FunctionTemplate> event_target_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> node_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_iframe_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> text_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> document_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> document_fragment_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> node_list_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_collection_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> token_list_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> dataset_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  auto new_event_template =
      [isolate](EventInterface interface) {
        return v8::FunctionTemplate::New(
            isolate, EventConstructor,
            v8::Integer::New(isolate, static_cast<int>(interface)));
      };
  v8::Local<v8::FunctionTemplate> event_template =
      new_event_template(EventInterface::Event);
  v8::Local<v8::FunctionTemplate> mouse_event_template =
      new_event_template(EventInterface::MouseEvent);
  v8::Local<v8::FunctionTemplate> pointer_event_template =
      new_event_template(EventInterface::PointerEvent);
  v8::Local<v8::FunctionTemplate> keyboard_event_template =
      new_event_template(EventInterface::KeyboardEvent);
  v8::Local<v8::FunctionTemplate> input_event_template =
      new_event_template(EventInterface::InputEvent);
  v8::Local<v8::FunctionTemplate> focus_event_template =
      new_event_template(EventInterface::FocusEvent);

  event_target_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "EventTarget"));
  node_template->SetClassName(v8::String::NewFromUtf8Literal(isolate, "Node"));
  element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "Element"));
  html_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLElement"));
  html_iframe_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLIFrameElement"));
  text_template->SetClassName(v8::String::NewFromUtf8Literal(isolate, "Text"));
  document_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "Document"));
  document_fragment_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "DocumentFragment"));
  node_list_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "NodeList"));
  html_collection_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLCollection"));
  token_list_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "DOMTokenList"));
  dataset_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "DOMStringMap"));
  event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "Event"));
  mouse_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "MouseEvent"));
  pointer_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "PointerEvent"));
  keyboard_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "KeyboardEvent"));
  input_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "InputEvent"));
  focus_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "FocusEvent"));
  node_template->Inherit(event_target_template);
  element_template->Inherit(node_template);
  html_element_template->Inherit(element_template);
  html_iframe_element_template->Inherit(html_element_template);
  text_template->Inherit(node_template);
  document_template->Inherit(node_template);
  document_fragment_template->Inherit(node_template);
  mouse_event_template->Inherit(event_template);
  pointer_event_template->Inherit(mouse_event_template);
  keyboard_event_template->Inherit(event_template);
  input_event_template->Inherit(event_template);
  focus_event_template->Inherit(event_template);
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {node_template, element_template, html_element_template, text_template,
        document_template, document_fragment_template}) {
    interface_template->InstanceTemplate()->SetInternalFieldCount(
        kNodeInternalFieldCount);
  }
  for (v8::Local<v8::FunctionTemplate> facade_template :
       {node_list_template, html_collection_template, token_list_template,
        dataset_template}) {
    facade_template->InstanceTemplate()->SetInternalFieldCount(
        kFacadeInternalFieldCount);
  }
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {event_template, mouse_event_template, pointer_event_template,
        keyboard_event_template, input_event_template, focus_event_template}) {
    interface_template->InstanceTemplate()->SetInternalFieldCount(1);
  }

  auto facade_data = [isolate](FacadeKind kind) {
    return v8::Integer::New(isolate, static_cast<int>(kind));
  };
  auto iterator_data = [isolate](FacadeKind kind, IteratorMode mode) {
    return v8::Integer::New(isolate, static_cast<int>(kind) * 10 +
                                         static_cast<int>(mode));
  };
  auto install_iterable =
      [isolate, facade_data, iterator_data](
          v8::Local<v8::FunctionTemplate> facade_template, FacadeKind kind,
          bool install_for_each) {
        v8::Local<v8::ObjectTemplate> prototype =
            facade_template->PrototypeTemplate();
        v8::Local<v8::ObjectTemplate> instance =
            facade_template->InstanceTemplate();
        for (v8::Local<v8::ObjectTemplate> surface : {prototype, instance}) {
          surface->SetNativeDataProperty(
              v8::String::NewFromUtf8Literal(isolate, "length"),
              FacadeLengthGetter, nullptr, facade_data(kind), v8::DontEnum);
        }
        prototype->Set(
            isolate, "item",
            v8::FunctionTemplate::New(isolate, FacadeItem, facade_data(kind)));
        if (install_for_each) {
          prototype->Set(
              isolate, "forEach",
              v8::FunctionTemplate::New(isolate, FacadeForEach,
                                        facade_data(kind)));
        }
        for (const auto &method :
             {std::pair<const char *, IteratorMode>{"keys",
                                                    IteratorMode::Keys},
              {"values", IteratorMode::Values},
              {"entries", IteratorMode::Entries}}) {
          prototype->Set(
              isolate, method.first,
              v8::FunctionTemplate::New(isolate, FacadeIterator,
                                        iterator_data(kind, method.second)));
        }
        prototype->Set(
            v8::Symbol::GetIterator(isolate),
            v8::FunctionTemplate::New(isolate, FacadeIterator,
                                      iterator_data(kind,
                                                    IteratorMode::Values)));
        instance->SetHandler(v8::IndexedPropertyHandlerConfiguration(
            FacadeIndexedGetter, nullptr, FacadeIndexedQuery, nullptr,
            FacadeIndexedEnumerator, facade_data(kind)));
      };
  install_iterable(node_list_template, FacadeKind::NodeList, true);
  install_iterable(html_collection_template, FacadeKind::HTMLCollection,
                   false);
  install_iterable(token_list_template, FacadeKind::ClassList, true);
  html_collection_template->PrototypeTemplate()->Set(
      isolate, "namedItem",
      v8::FunctionTemplate::New(isolate, HTMLCollectionNamedItem));
  html_collection_template->InstanceTemplate()->SetHandler(
      v8::NamedPropertyHandlerConfiguration(
          HTMLCollectionNamedGetter, nullptr, HTMLCollectionNamedQuery,
          nullptr, HTMLCollectionNamedEnumerator, v8::Local<v8::Value>(),
          static_cast<v8::PropertyHandlerFlags>(
              static_cast<int>(v8::PropertyHandlerFlags::kNonMasking) |
              static_cast<int>(
                  v8::PropertyHandlerFlags::kOnlyInterceptStrings))));
  for (v8::Local<v8::ObjectTemplate> surface :
       {token_list_template->PrototypeTemplate(),
        token_list_template->InstanceTemplate()}) {
    surface->SetNativeDataProperty(
        v8::String::NewFromUtf8Literal(isolate, "value"),
        ClassListValueGetter, ClassListValueSetter, v8::Local<v8::Value>(),
        v8::DontEnum);
  }
  for (const auto &method :
       {std::pair<const char *, v8::FunctionCallback>{"contains",
                                                     ClassListContains},
        {"add", ClassListAdd},
        {"remove", ClassListRemove},
        {"toggle", ClassListToggle},
        {"replace", ClassListReplace},
        {"toString", ClassListToString}}) {
    token_list_template->PrototypeTemplate()->Set(
        isolate, method.first,
        v8::FunctionTemplate::New(isolate, method.second));
  }
  dataset_template->InstanceTemplate()->SetHandler(
      v8::NamedPropertyHandlerConfiguration(
          DatasetNamedGetter, DatasetNamedSetter, DatasetNamedQuery,
          DatasetNamedDeleter, DatasetNamedEnumerator,
          v8::Local<v8::Value>(),
          v8::PropertyHandlerFlags::kOnlyInterceptStrings));

  v8::Local<v8::ObjectTemplate> collection_iterator_template =
      v8::ObjectTemplate::New(isolate);
  collection_iterator_template->SetInternalFieldCount(4);
  collection_iterator_template->Set(
      isolate, "next",
      v8::FunctionTemplate::New(isolate, CollectionIteratorNext));
  collection_iterator_template->Set(
      v8::Symbol::GetIterator(isolate),
      v8::FunctionTemplate::New(isolate, CollectionIteratorSelf));
  realm->node_list_template.Reset(isolate, node_list_template);
  realm->html_collection_template.Reset(isolate, html_collection_template);
  realm->token_list_template.Reset(isolate, token_list_template);
  realm->dataset_template.Reset(isolate, dataset_template);
  realm->collection_iterator_template.Reset(isolate,
                                             collection_iterator_template);

  v8::Local<v8::ObjectTemplate> event_target_prototype =
      event_target_template->PrototypeTemplate();
  event_target_prototype->Set(
      isolate, "addEventListener",
      v8::FunctionTemplate::New(isolate, NodeAddEventListener));
  event_target_prototype->Set(
      isolate, "removeEventListener",
      v8::FunctionTemplate::New(isolate, NodeRemoveEventListener));
  event_target_prototype->Set(
      isolate, "dispatchEvent",
      v8::FunctionTemplate::New(isolate, EventTargetDispatchEvent));

  auto install_event_surface =
      [isolate](v8::Local<v8::ObjectTemplate> object) {
        for (const char *name : {"type", "target", "currentTarget",
                                 "eventPhase", "bubbles", "cancelable",
                                 "composed", "defaultPrevented", "isTrusted",
                                 "timeStamp", "relatedTarget"}) {
          object->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              EventPropertyGetter);
        }
        for (const char *name : {"cancelBubble", "returnValue"}) {
          object->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              EventPropertyGetter, EventPropertySetter);
        }
        object->Set(isolate, "stopPropagation",
                    v8::FunctionTemplate::New(isolate,
                                              EventStopPropagation));
        object->Set(isolate, "stopImmediatePropagation",
                    v8::FunctionTemplate::New(
                        isolate, EventStopImmediatePropagation));
        object->Set(isolate, "preventDefault",
                    v8::FunctionTemplate::New(isolate, EventPreventDefault));
        object->Set(isolate, "composedPath",
                    v8::FunctionTemplate::New(isolate, EventComposedPath));
      };
  auto install_mouse_event_surface =
      [isolate](v8::Local<v8::ObjectTemplate> object) {
        for (const char *name : {"clientX", "clientY", "button", "buttons",
                                 "altKey", "ctrlKey", "metaKey",
                                 "shiftKey"}) {
          object->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              EventPropertyGetter);
        }
      };
  auto install_pointer_event_surface =
      [isolate](v8::Local<v8::ObjectTemplate> object) {
        for (const char *name : {"pointerId", "pointerType", "isPrimary"}) {
          object->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              EventPropertyGetter);
        }
      };
  auto install_keyboard_event_surface =
      [isolate](v8::Local<v8::ObjectTemplate> object) {
        for (const char *name : {"key", "code", "repeat", "isComposing",
                                 "altKey", "ctrlKey", "metaKey",
                                 "shiftKey"}) {
          object->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              EventPropertyGetter);
        }
      };
  auto install_input_event_surface =
      [isolate](v8::Local<v8::ObjectTemplate> object) {
        for (const char *name : {"data", "inputType", "isComposing"}) {
          object->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              EventPropertyGetter);
        }
      };

  install_event_surface(event_template->PrototypeTemplate());
  install_mouse_event_surface(mouse_event_template->PrototypeTemplate());
  install_pointer_event_surface(pointer_event_template->PrototypeTemplate());
  install_keyboard_event_surface(keyboard_event_template->PrototypeTemplate());
  install_input_event_surface(input_event_template->PrototypeTemplate());
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {event_template, mouse_event_template, pointer_event_template,
        keyboard_event_template, input_event_template, focus_event_template}) {
    install_event_surface(interface_template->InstanceTemplate());
  }
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {mouse_event_template, pointer_event_template}) {
    install_mouse_event_surface(interface_template->InstanceTemplate());
  }
  install_pointer_event_surface(pointer_event_template->InstanceTemplate());
  install_keyboard_event_surface(keyboard_event_template->InstanceTemplate());
  install_input_event_surface(input_event_template->InstanceTemplate());
  for (const auto &constant :
       {std::pair<const char *, int>{"NONE", kEventPhaseNone},
        {"CAPTURING_PHASE", kEventPhaseCapturing},
        {"AT_TARGET", kEventPhaseAtTarget},
        {"BUBBLING_PHASE", kEventPhaseBubbling}}) {
    event_template->Set(
        v8::String::NewFromUtf8(isolate, constant.first).ToLocalChecked(),
        v8::Integer::New(isolate, constant.second),
        static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontDelete));
    event_template->PrototypeTemplate()->Set(
        v8::String::NewFromUtf8(isolate, constant.first).ToLocalChecked(),
        v8::Integer::New(isolate, constant.second),
        static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontDelete));
  }

  v8::Local<v8::ObjectTemplate> node_prototype =
      node_template->PrototypeTemplate();
  node_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "textContent"),
      NodeTextContentGetter, NodeTextContentSetter);
  node_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "nodeValue"), NodeValueGetter,
      NodeValueSetter);
  for (const char *name : {"nodeType", "nodeName", "localName",
                           "namespaceURI", "prefix", "isConnected"}) {
    node_prototype->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeMetadataGetter);
  }
  node_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "ownerDocument"),
      NodeOwnerDocumentGetter);
  node_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "baseURI"), NodeBaseURIGetter);
  for (const char *name : {"parentNode", "parentElement", "firstChild",
                           "lastChild", "previousSibling", "nextSibling"}) {
    node_prototype->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeRelationGetter);
  }
  node_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "childNodes"),
      NodeChildrenGetter);
  node_prototype->Set(isolate, "appendChild",
                      v8::FunctionTemplate::New(isolate, NodeAppendChild));
  node_prototype->Set(isolate, "insertBefore",
                      v8::FunctionTemplate::New(isolate, NodeInsertBefore));
  node_prototype->Set(isolate, "removeChild",
                      v8::FunctionTemplate::New(isolate, NodeRemoveChild));
  node_prototype->Set(isolate, "replaceChild",
                      v8::FunctionTemplate::New(isolate, NodeReplaceChild));
  node_prototype->Set(isolate, "hasChildNodes",
                      v8::FunctionTemplate::New(isolate, NodeHasChildNodes));
  node_prototype->Set(isolate, "contains",
                      v8::FunctionTemplate::New(isolate, NodeContains));
  node_prototype->Set(isolate, "remove",
                      v8::FunctionTemplate::New(isolate, NodeRemove));
  node_prototype->Set(isolate, "cloneNode",
                      v8::FunctionTemplate::New(isolate, NodeCloneNode));

  auto install_parent_node_surface =
      [isolate](v8::Local<v8::ObjectTemplate> prototype) {
        for (const char *name : {"firstElementChild", "lastElementChild"}) {
          prototype->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              NodeRelationGetter);
        }
        prototype->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "children"),
            NodeChildrenGetter);
        prototype->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "childElementCount"),
            NodeChildElementCountGetter);
      };

  v8::Local<v8::ObjectTemplate> element_prototype =
      element_template->PrototypeTemplate();
  install_parent_node_surface(element_prototype);
  for (const char *name : {"tagName"}) {
    element_prototype->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeMetadataGetter);
  }
  for (const char *name : {"previousElementSibling", "nextElementSibling"}) {
    element_prototype->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeRelationGetter);
  }
  element_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "id"),
      NodeReflectedAttributeGetter, NodeReflectedAttributeSetter);
  element_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "className"),
      NodeReflectedAttributeGetter, NodeReflectedAttributeSetter);
  element_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "style"), NodeStyleGetter);
  element_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "classList"),
      NodeClassListGetter);
  element_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "dataset"), NodeDatasetGetter);
  element_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "innerHTML"),
      ElementInnerHTMLGetter, ElementInnerHTMLSetter);
  element_prototype->Set(isolate, "getAttribute",
                         v8::FunctionTemplate::New(isolate, NodeGetAttribute));
  element_prototype->Set(isolate, "setAttribute",
                         v8::FunctionTemplate::New(isolate, NodeSetAttribute));
  element_prototype->Set(
      isolate, "removeAttribute",
      v8::FunctionTemplate::New(isolate, NodeRemoveAttribute));
  element_prototype->Set(isolate, "hasAttribute",
                         v8::FunctionTemplate::New(isolate, NodeHasAttribute));
  element_prototype->Set(
      isolate, "getAttributeNames",
      v8::FunctionTemplate::New(isolate, NodeGetAttributeNames));
  element_prototype->Set(
      isolate, "querySelector",
      v8::FunctionTemplate::New(isolate, NodeQuerySelector,
                                v8::False(isolate)));
  element_prototype->Set(
      isolate, "querySelectorAll",
      v8::FunctionTemplate::New(isolate, NodeQuerySelector,
                                v8::True(isolate)));
  element_prototype->Set(
      isolate, "matches",
      v8::FunctionTemplate::New(isolate, ElementMatchesSelector));
  element_prototype->Set(
      isolate, "closest",
      v8::FunctionTemplate::New(isolate, ElementClosestSelector));
  element_prototype->Set(
      isolate, "insertAdjacentHTML",
      v8::FunctionTemplate::New(isolate, ElementInsertAdjacentHTML));

  v8::Local<v8::ObjectTemplate> html_element_prototype =
      html_element_template->PrototypeTemplate();
  html_element_prototype->SetAccessorProperty(
      v8::String::NewFromUtf8Literal(isolate, "value"),
      v8::FunctionTemplate::New(isolate, ElementFormValueFunctionGetter),
      v8::FunctionTemplate::New(isolate, ElementFormValueFunctionSetter));
  html_element_prototype->SetAccessorProperty(
      v8::String::NewFromUtf8Literal(isolate, "checked"),
      v8::FunctionTemplate::New(isolate, ElementFormCheckedFunctionGetter),
      v8::FunctionTemplate::New(isolate,
                                ElementFormCheckedFunctionSetter));
  for (const char *name : {"defaultValue", "name", "type", "placeholder",
                           "title", "lang", "dir", "htmlFor"}) {
    html_element_prototype->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeReflectedAttributeGetter, NodeReflectedAttributeSetter);
  }
  for (const char *name : {"defaultChecked", "disabled", "multiple",
                           "required", "readOnly"}) {
    html_element_prototype->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeReflectedBooleanGetter, NodeReflectedBooleanSetter);
  }
  html_element_prototype->Set(
      isolate, "focus",
      v8::FunctionTemplate::New(isolate, HTMLElementFocus,
                                v8::True(isolate)));
  html_element_prototype->Set(
      isolate, "blur",
      v8::FunctionTemplate::New(isolate, HTMLElementFocus,
                                v8::False(isolate)));

  text_template->PrototypeTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "data"), NodeValueGetter,
      NodeValueSetter);

  v8::Local<v8::ObjectTemplate> document_prototype =
      document_template->PrototypeTemplate();
  install_parent_node_surface(document_prototype);
  for (const char *name : {"documentElement", "head", "body"}) {
    document_prototype->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeRelationGetter);
  }
  document_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "defaultView"),
      DocumentDefaultViewGetter);
  document_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "activeElement"),
      DocumentActiveElementGetter);
  document_prototype->Set(
      isolate, "getElementById",
      v8::FunctionTemplate::New(isolate, DocumentGetElementByID));
  document_prototype->Set(
      isolate, "querySelector",
      v8::FunctionTemplate::New(isolate, NodeQuerySelector,
                                v8::False(isolate)));
  document_prototype->Set(
      isolate, "querySelectorAll",
      v8::FunctionTemplate::New(isolate, NodeQuerySelector,
                                v8::True(isolate)));
  document_prototype->Set(
      isolate, "createElement",
      v8::FunctionTemplate::New(isolate, DocumentCreateElement));
  document_prototype->Set(
      isolate, "createElementNS",
      v8::FunctionTemplate::New(isolate, DocumentCreateElementNS));
  document_prototype->Set(
      isolate, "createTextNode",
      v8::FunctionTemplate::New(isolate, DocumentCreateTextNode));
  document_prototype->Set(
      isolate, "createDocumentFragment",
      v8::FunctionTemplate::New(isolate, DocumentCreateDocumentFragment));

  install_parent_node_surface(
      document_fragment_template->PrototypeTemplate());
  document_fragment_template->PrototypeTemplate()->Set(
      isolate, "querySelector",
      v8::FunctionTemplate::New(isolate, NodeQuerySelector,
                                v8::False(isolate)));
  document_fragment_template->PrototypeTemplate()->Set(
      isolate, "querySelectorAll",
      v8::FunctionTemplate::New(isolate, NodeQuerySelector,
                                v8::True(isolate)));

  // Current V8 native-data callbacks expose Holder rather than the original
  // receiver. Keep the standards-shaped accessors on the prototypes for
  // reflection, and mirror them onto each concrete instance template so host
  // calls always receive the wrapper carrying the native NodeHandle fields.
  auto install_node_instance_surface =
      [isolate](v8::Local<v8::ObjectTemplate> instance) {
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "textContent"),
            NodeTextContentGetter, NodeTextContentSetter);
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "nodeValue"),
            NodeValueGetter, NodeValueSetter);
        for (const char *name : {"nodeType", "nodeName", "localName",
                                 "namespaceURI", "prefix", "isConnected"}) {
          instance->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              NodeMetadataGetter);
        }
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "ownerDocument"),
            NodeOwnerDocumentGetter);
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "baseURI"),
            NodeBaseURIGetter);
        for (const char *name : {"parentNode", "parentElement", "firstChild",
                                 "lastChild", "previousSibling",
                                 "nextSibling"}) {
          instance->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              NodeRelationGetter);
        }
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "childNodes"),
            NodeChildrenGetter);
      };
  auto install_parent_instance_surface =
      [isolate](v8::Local<v8::ObjectTemplate> instance) {
        for (const char *name : {"firstElementChild", "lastElementChild"}) {
          instance->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              NodeRelationGetter);
        }
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "children"),
            NodeChildrenGetter);
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "childElementCount"),
            NodeChildElementCountGetter);
      };
  auto install_element_instance_surface =
      [isolate, install_parent_instance_surface](
          v8::Local<v8::ObjectTemplate> instance) {
        install_parent_instance_surface(instance);
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "tagName"),
            NodeMetadataGetter);
        for (const char *name : {"previousElementSibling",
                                 "nextElementSibling"}) {
          instance->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              NodeRelationGetter);
        }
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "id"),
            NodeReflectedAttributeGetter, NodeReflectedAttributeSetter);
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "className"),
            NodeReflectedAttributeGetter, NodeReflectedAttributeSetter);
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "style"), NodeStyleGetter);
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "classList"),
            NodeClassListGetter);
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "dataset"),
            NodeDatasetGetter);
        instance->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "innerHTML"),
            ElementInnerHTMLGetter, ElementInnerHTMLSetter);
      };
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {node_template, element_template, html_element_template, text_template,
        document_template, document_fragment_template}) {
    install_node_instance_surface(interface_template->InstanceTemplate());
  }
  install_element_instance_surface(element_template->InstanceTemplate());
  install_element_instance_surface(html_element_template->InstanceTemplate());
  html_element_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "value"),
      ElementFormValueGetter, ElementFormValueSetter);
  html_element_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "checked"),
      ElementFormCheckedGetter, ElementFormCheckedSetter);
  for (const char *name : {"defaultValue", "name", "type", "placeholder",
                           "title", "lang", "dir", "htmlFor"}) {
    html_element_template->InstanceTemplate()->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeReflectedAttributeGetter, NodeReflectedAttributeSetter);
  }
  for (const char *name : {"defaultChecked", "disabled", "multiple",
                           "required", "readOnly"}) {
    html_element_template->InstanceTemplate()->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeReflectedBooleanGetter, NodeReflectedBooleanSetter);
  }
  text_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "data"), NodeValueGetter,
      NodeValueSetter);
  install_parent_instance_surface(document_template->InstanceTemplate());
  install_parent_instance_surface(
      document_fragment_template->InstanceTemplate());
  for (const char *name : {"documentElement", "head", "body"}) {
    document_template->InstanceTemplate()->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeRelationGetter);
  }
  document_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "defaultView"),
      DocumentDefaultViewGetter);
  document_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "activeElement"),
      DocumentActiveElementGetter);

  realm->event_target_template.Reset(isolate, event_target_template);
  realm->node_template.Reset(isolate, node_template);
  realm->element_template.Reset(isolate, element_template);
  realm->html_element_template.Reset(isolate, html_element_template);
  realm->text_template.Reset(isolate, text_template);
  realm->document_template.Reset(isolate, document_template);
  realm->document_fragment_template.Reset(isolate,
                                           document_fragment_template);
  realm->event_template.Reset(isolate, event_template);
  realm->mouse_event_template.Reset(isolate, mouse_event_template);
  realm->pointer_event_template.Reset(isolate, pointer_event_template);
  realm->keyboard_event_template.Reset(isolate, keyboard_event_template);
  realm->input_event_template.Reset(isolate, input_event_template);
  realm->focus_event_template.Reset(isolate, focus_event_template);

  v8::Local<v8::ObjectTemplate> style_template =
      v8::ObjectTemplate::New(isolate);
  style_template->SetInternalFieldCount(1);
  style_template->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "cssText"), StyleCSSTextGetter,
      StyleCSSTextSetter);
  style_template->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "length"), StyleLengthGetter);
  style_template->Set(isolate, "item",
                      v8::FunctionTemplate::New(isolate, StyleItem));
  style_template->Set(
      isolate, "getPropertyValue",
      v8::FunctionTemplate::New(isolate, StyleGetPropertyValue));
  style_template->Set(
      isolate, "getPropertyPriority",
      v8::FunctionTemplate::New(isolate, StyleGetPropertyPriority));
  style_template->Set(isolate, "setProperty",
                      v8::FunctionTemplate::New(isolate, StyleSetProperty));
  style_template->Set(
      isolate, "removeProperty",
      v8::FunctionTemplate::New(isolate, StyleRemoveProperty));
  for (const char *name : {
           "display",          "color",              "background",
           "backgroundColor",  "fontSize",           "fontWeight",
           "lineHeight",       "textDecoration",     "textDecorationLine",
           "textAlign",        "opacity",            "width",
           "height",           "minWidth",           "maxWidth",
           "padding",          "paddingTop",         "paddingRight",
           "paddingBottom",    "paddingLeft",        "margin",
           "marginTop",        "marginRight",        "marginBottom",
           "marginLeft",       "border",             "borderTop",
           "borderRight",      "borderBottom",       "borderLeft",
           "borderWidth",      "borderStyle",        "borderColor",
           "borderTopWidth",   "borderRightWidth",   "borderBottomWidth",
           "borderLeftWidth",  "borderTopStyle",     "borderRightStyle",
           "borderBottomStyle", "borderLeftStyle",    "borderTopColor",
           "borderRightColor", "borderBottomColor",  "borderLeftColor",
           "listStyle",        "listStyleType",      "cssFloat"}) {
    style_template->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        StyleDirectPropertyGetter, StyleDirectPropertySetter);
  }
  realm->style_template.Reset(isolate, style_template);

  v8::Local<v8::Function> queue_microtask;
  v8::Local<v8::Function> set_timeout;
  v8::Local<v8::Function> clear_timeout;
  if (!v8::Function::New(context, QueueMicrotaskCallback)
           .ToLocal(&queue_microtask) ||
      !v8::Function::New(context, SetTimeoutCallback).ToLocal(&set_timeout) ||
      !v8::Function::New(context, ClearTimeoutCallback)
           .ToLocal(&clear_timeout)) {
    return false;
  }
  v8::Local<v8::Object> global = context->Global();
  auto expose_interface =
      [context, global, isolate](
          const char *name,
          v8::Local<v8::FunctionTemplate> interface_template) {
        v8::Local<v8::Function> constructor;
        return interface_template->GetFunction(context).ToLocal(&constructor) &&
               global
                   ->Set(context,
                         v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
                         constructor)
                   .FromMaybe(false);
      };
  return expose_interface("EventTarget", event_target_template) &&
         expose_interface("Event", event_template) &&
         expose_interface("MouseEvent", mouse_event_template) &&
         expose_interface("PointerEvent", pointer_event_template) &&
         expose_interface("KeyboardEvent", keyboard_event_template) &&
         expose_interface("InputEvent", input_event_template) &&
         expose_interface("FocusEvent", focus_event_template) &&
         expose_interface("Node", node_template) &&
         expose_interface("Element", element_template) &&
         expose_interface("HTMLElement", html_element_template) &&
         expose_interface("HTMLIFrameElement", html_iframe_element_template) &&
         expose_interface("Text", text_template) &&
         expose_interface("Document", document_template) &&
         expose_interface("DocumentFragment", document_fragment_template) &&
         expose_interface("NodeList", node_list_template) &&
         expose_interface("HTMLCollection", html_collection_template) &&
         expose_interface("DOMTokenList", token_list_template) &&
         expose_interface("DOMStringMap", dataset_template) &&
         global
             ->Set(context, v8::String::NewFromUtf8Literal(isolate, "window"),
                   global)
             .FromMaybe(false) &&
         global
             ->Set(context, v8::String::NewFromUtf8Literal(isolate, "self"),
                   global)
             .FromMaybe(false) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "queueMicrotask"),
                   queue_microtask)
             .FromMaybe(false) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "setTimeout"),
                   set_timeout)
             .FromMaybe(false) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "clearTimeout"),
                   clear_timeout)
             .FromMaybe(false);
}

bool ConfigureNativeEvent(const gossamer_v8_input_event *input,
                          EventState *state, std::string *error) {
  if (input == nullptr) {
    *error = "V8 received a null browser event";
    return false;
  }
  state->trusted = true;
  state->timestamp = static_cast<double>(MonotonicNanos()) / 1000000.0;
  switch (input->type) {
  case 1:
    state->type = "click";
    state->interface = EventInterface::MouseEvent;
    state->bubbles = true;
    state->cancelable = true;
    state->composed = true;
    break;
  case 2:
    state->type = "pointerdown";
    state->interface = EventInterface::PointerEvent;
    state->bubbles = state->cancelable = state->composed = true;
    break;
  case 3:
    state->type = "pointerup";
    state->interface = EventInterface::PointerEvent;
    state->bubbles = state->cancelable = state->composed = true;
    break;
  case 4:
    state->type = "pointermove";
    state->interface = EventInterface::PointerEvent;
    state->bubbles = state->cancelable = state->composed = true;
    break;
  case 5:
    state->type = "pointercancel";
    state->interface = EventInterface::PointerEvent;
    state->bubbles = state->composed = true;
    break;
  case 6:
    state->type = "pointerover";
    state->interface = EventInterface::PointerEvent;
    state->bubbles = state->cancelable = state->composed = true;
    break;
  case 7:
    state->type = "pointerout";
    state->interface = EventInterface::PointerEvent;
    state->bubbles = state->cancelable = state->composed = true;
    break;
  case 8:
    state->type = "pointerenter";
    state->interface = EventInterface::PointerEvent;
    break;
  case 9:
    state->type = "pointerleave";
    state->interface = EventInterface::PointerEvent;
    break;
  case 10:
    state->type = "keydown";
    state->interface = EventInterface::KeyboardEvent;
    state->bubbles = state->cancelable = state->composed = true;
    break;
  case 11:
    state->type = "keyup";
    state->interface = EventInterface::KeyboardEvent;
    state->bubbles = state->cancelable = state->composed = true;
    break;
  case 12:
    state->type = "beforeinput";
    state->interface = EventInterface::InputEvent;
    state->bubbles = state->cancelable = state->composed = true;
    break;
  case 13:
    state->type = "input";
    state->interface = EventInterface::InputEvent;
    state->bubbles = state->composed = true;
    break;
  case 14:
    state->type = "focus";
    state->interface = EventInterface::FocusEvent;
    state->composed = true;
    break;
  case 15:
    state->type = "blur";
    state->interface = EventInterface::FocusEvent;
    state->composed = true;
    break;
  case 16:
    state->type = "focusin";
    state->interface = EventInterface::FocusEvent;
    state->bubbles = state->composed = true;
    break;
  case 17:
    state->type = "focusout";
    state->interface = EventInterface::FocusEvent;
    state->bubbles = state->composed = true;
    break;
  case 18:
    state->type = "change";
    state->interface = EventInterface::Event;
    state->bubbles = true;
    break;
  default:
    *error = "V8 received an unsupported browser event type";
    return false;
  }
  auto assign = [](const char *data, size_t length, std::string *output) {
    if (data == nullptr) {
      output->clear();
      return;
    }
    output->assign(data, length);
  };
  state->client_x = input->x;
  state->client_y = input->y;
  state->button = input->button;
  state->buttons = input->buttons;
  state->pointer_id = input->pointer_id;
  state->is_primary = input->is_primary != 0;
  state->repeat = input->repeat != 0;
  state->is_composing = input->is_composing != 0;
  state->alt_key = input->alt_key != 0;
  state->ctrl_key = input->ctrl_key != 0;
  state->meta_key = input->meta_key != 0;
  state->shift_key = input->shift_key != 0;
  assign(input->pointer_type, input->pointer_type_length,
         &state->pointer_type);
  assign(input->key, input->key_length, &state->key);
  assign(input->code, input->code_length, &state->code);
  assign(input->data, input->data_length, &state->data);
  assign(input->input_type, input->input_type_length, &state->input_type);
  if (input->related_document != 0 && input->related_node != 0) {
    state->has_related_target = true;
    state->related_target =
        WrapperKey{input->related_document, input->related_node};
  }
  return true;
}

void ClearRealmHandles(gossamer_v8_realm *realm) {
  for (auto &wrapper : realm->wrappers) {
    if (wrapper.second.object.IsWeak())
      wrapper.second.object.ClearWeak<WrapperWeakData>();
    wrapper.second.object.Reset();
    delete wrapper.second.weak;
    wrapper.second.weak = nullptr;
  }
  realm->wrappers.clear();
  for (auto &listeners : realm->listeners) {
    for (auto &listener : listeners.second)
      listener->callback.Reset();
  }
  realm->listeners.clear();
  realm->event_listener_count = 0;
  for (EventWeakData *event : realm->events) {
    if (event->object.IsWeak())
      event->object.ClearWeak<EventWeakData>();
    event->object.Reset();
    delete event->state;
    delete event;
  }
  realm->events.clear();
  for (auto &callback : realm->callbacks)
    callback.second.Reset();
  realm->callbacks.clear();
  realm->timer_callbacks.clear();
  realm->callback_timers.clear();
  realm->event_target_template.Reset();
  realm->event_template.Reset();
  realm->mouse_event_template.Reset();
  realm->pointer_event_template.Reset();
  realm->keyboard_event_template.Reset();
  realm->input_event_template.Reset();
  realm->focus_event_template.Reset();
  realm->node_list_template.Reset();
  realm->html_collection_template.Reset();
  realm->token_list_template.Reset();
  realm->dataset_template.Reset();
  realm->node_template.Reset();
  realm->element_template.Reset();
  realm->html_element_template.Reset();
  realm->text_template.Reset();
  realm->document_template.Reset();
  realm->document_fragment_template.Reset();
  realm->style_template.Reset();
  realm->collection_iterator_template.Reset();
  realm->document_wrapper.Reset();
  realm->document_bound = false;
  realm->document_key = WrapperKey{};
  realm->base_uri.clear();
}

} // namespace
extern "C" int gossamer_v8_initialize(const char *icu_data_path,
                                      char **error_out) {
  std::call_once(runtime_once, InitializeRuntime, icu_data_path);
  if (!runtime_ready) {
    SetError(error_out, runtime_error.empty() ? "V8 runtime is unavailable"
                                              : runtime_error);
    return 0;
  }
  return 1;
}

extern "C" const char *gossamer_v8_version(void) {
  return v8::V8::GetVersion();
}

extern "C" gossamer_v8_realm *gossamer_v8_realm_new(uint64_t sampling_interval,
                                                    char **error_out) {
  if (!runtime_ready) {
    SetError(error_out, "V8 runtime is not initialized");
    return nullptr;
  }

  std::unique_ptr<gossamer_v8_realm> realm(new gossamer_v8_realm());
  realm->allocator.reset(v8::ArrayBuffer::Allocator::NewDefaultAllocator());
  if (!realm->allocator) {
    SetError(error_out, "V8 failed to create its ArrayBuffer allocator");
    return nullptr;
  }

  v8::Isolate::CreateParams create_params;
  create_params.array_buffer_allocator = realm->allocator.get();
  realm->isolate = v8::Isolate::New(create_params);
  if (realm->isolate == nullptr) {
    SetError(error_out, "V8 failed to create an isolate");
    return nullptr;
  }

  bool realm_ready = false;
  {
    v8::Locker locker(realm->isolate);
    v8::Isolate::Scope isolate_scope(realm->isolate);
    v8::HandleScope handle_scope(realm->isolate);
    realm->isolate->SetData(0, realm.get());
    realm->isolate->SetMicrotasksPolicy(v8::MicrotasksPolicy::kExplicit);
    v8::Local<v8::Context> context = v8::Context::New(realm->isolate);
    if (!context.IsEmpty()) {
      v8::Context::Scope context_scope(context);
      if (InstallBindings(realm.get(), context)) {
        realm->context.Reset(realm->isolate, context);
        realm->isolate->AddGCPrologueCallback(GCPrologue, realm.get());
        realm->isolate->AddGCEpilogueCallback(GCEpilogue, realm.get());
        realm_ready = true;
      }
    }
    if (realm_ready && sampling_interval != 0) {
      auto flags = static_cast<v8::HeapProfiler::SamplingFlags>(
          v8::HeapProfiler::kSamplingIncludeObjectsCollectedByMajorGC |
          v8::HeapProfiler::kSamplingIncludeObjectsCollectedByMinorGC);
      realm->sampling =
          realm->isolate->GetHeapProfiler()->StartSamplingHeapProfiler(
              sampling_interval, 16, flags);
      if (!realm->sampling) {
        realm->isolate->RemoveGCPrologueCallback(GCPrologue, realm.get());
        realm->isolate->RemoveGCEpilogueCallback(GCEpilogue, realm.get());
        ClearRealmHandles(realm.get());
        realm->context.Reset();
        realm_ready = false;
      }
    }
  }
  if (!realm_ready) {
    realm->isolate->SetData(0, nullptr);
    v8::platform::NotifyIsolateShutdown(runtime_platform.get(), realm->isolate);
    realm->isolate->Dispose();
    realm->isolate = nullptr;
    SetError(error_out, sampling_interval != 0
                            ? "V8 failed to create a context or start profiling"
                            : "V8 failed to create a context and DOM bindings");
    return nullptr;
  }
  return realm.release();
}

extern "C" int
gossamer_v8_realm_evaluate(gossamer_v8_realm *realm,
                           const gossamer_v8_host *host, const char *source,
                           size_t source_length, const char *source_url,
                           size_t source_url_length, char **error_out) {
  if (!RequireRealm(realm, error_out))
    return 0;
  if (source_length > static_cast<size_t>(std::numeric_limits<int>::max()) ||
      source_url_length >
          static_cast<size_t>(std::numeric_limits<int>::max())) {
    SetError(error_out, "V8 script source or URL exceeds the supported length");
    return 0;
  }
  if (source == nullptr)
    source = "";
  if (source_url == nullptr)
    source_url = "";
  std::lock_guard<std::mutex> guard(realm->mutex);
  if (!RequireRealm(realm, error_out))
    return 0;

  uint64_t started = MonotonicNanos();
  realm->evaluations.fetch_add(1, std::memory_order_relaxed);
  v8::Locker locker(realm->isolate);
  v8::Isolate::Scope isolate_scope(realm->isolate);
  v8::HandleScope handle_scope(realm->isolate);
  v8::Local<v8::Context> context = realm->context.Get(realm->isolate);
  v8::Context::Scope context_scope(context);
  HostScope host_scope(realm, host);
  std::string binding_error;
  if (!EnsureDocumentBinding(realm, context, &binding_error)) {
    SetError(error_out, binding_error);
    return 0;
  }
  v8::TryCatch caught(realm->isolate);

  v8::Local<v8::String> source_string;
  if (!v8::String::NewFromUtf8(realm->isolate, source,
                               v8::NewStringType::kNormal,
                               static_cast<int>(source_length))
           .ToLocal(&source_string)) {
    realm->evaluation_nanos.fetch_add(MonotonicNanos() - started,
                                      std::memory_order_relaxed);
    SetError(error_out, "V8 failed to allocate the script source");
    return 0;
  }

  v8::Local<v8::String> url_string;
  if (!v8::String::NewFromUtf8(realm->isolate, source_url,
                               v8::NewStringType::kNormal,
                               static_cast<int>(source_url_length))
           .ToLocal(&url_string)) {
    realm->evaluation_nanos.fetch_add(MonotonicNanos() - started,
                                      std::memory_order_relaxed);
    SetError(error_out, "V8 failed to allocate the script URL");
    return 0;
  }

  v8::ScriptOrigin origin(url_string);
  v8::Local<v8::Script> script;
  v8::Local<v8::Value> result;
  bool ok =
      v8::Script::Compile(context, source_string, &origin).ToLocal(&script) &&
      script->Run(context).ToLocal(&result);
  realm->evaluation_nanos.fetch_add(MonotonicNanos() - started,
                                    std::memory_order_relaxed);
  if (!ok) {
    SetError(error_out, DescribeException(realm->isolate, context, caught));
    return 0;
  }
  return 1;
}

extern "C" int gossamer_v8_realm_dispatch_event(gossamer_v8_realm *realm,
                                                const gossamer_v8_host *host,
                                                const gossamer_v8_input_event *input,
                                                int *default_prevented_out,
                                                char **error_out) {
  if (!RequireRealm(realm, error_out))
    return 0;
  std::lock_guard<std::mutex> guard(realm->mutex);
  if (!RequireRealm(realm, error_out))
    return 0;
  auto state = std::make_unique<EventState>();
  std::string error;
  if (!ConfigureNativeEvent(input, state.get(), &error)) {
    SetError(error_out, error);
    return 0;
  }

  v8::Locker locker(realm->isolate);
  v8::Isolate::Scope isolate_scope(realm->isolate);
  v8::HandleScope handle_scope(realm->isolate);
  v8::Local<v8::Context> context = realm->context.Get(realm->isolate);
  v8::Context::Scope context_scope(context);
  HostScope host_scope(realm, host);
  if (!EnsureDocumentBinding(realm, context, &error)) {
    SetError(error_out, error);
    return 0;
  }
  v8::Local<v8::Object> event_object;
  EventState *event_state = state.get();
  if (!NewEventObject(realm, context, std::move(state)).ToLocal(&event_object)) {
    SetError(error_out, "V8 failed to allocate a browser Event");
    return 0;
  }
  WrapperKey target{input->document, input->node};
  if (!DispatchEventState(realm, context, target, event_object, event_state,
                          &error)) {
    SetError(error_out, error);
    return 0;
  }
  if (default_prevented_out != nullptr)
    *default_prevented_out = event_state->default_prevented ? 1 : 0;
  return 1;
}

extern "C" int gossamer_v8_realm_invoke(gossamer_v8_realm *realm,
                                        const gossamer_v8_host *host,
                                        uint64_t callback, char **error_out) {
  if (!RequireRealm(realm, error_out))
    return 0;
  std::lock_guard<std::mutex> guard(realm->mutex);
  if (!RequireRealm(realm, error_out))
    return 0;

  v8::Locker locker(realm->isolate);
  v8::Isolate::Scope isolate_scope(realm->isolate);
  v8::HandleScope handle_scope(realm->isolate);
  v8::Local<v8::Context> context = realm->context.Get(realm->isolate);
  v8::Context::Scope context_scope(context);
  HostScope host_scope(realm, host);
  std::string binding_error;
  if (!EnsureDocumentBinding(realm, context, &binding_error)) {
    SetError(error_out, binding_error);
    return 0;
  }
  auto found = realm->callbacks.find(callback);
  if (found == realm->callbacks.end()) {
    SetError(error_out, "V8 callback handle is unknown or already consumed");
    return 0;
  }
  v8::Local<v8::Function> function = found->second.Get(realm->isolate);
  RemoveCallback(realm, callback);
  realm->callbacks_invoked.fetch_add(1, std::memory_order_relaxed);

  v8::TryCatch caught(realm->isolate);
  v8::Local<v8::Value> result;
  if (!function->Call(context, context->Global(), 0, nullptr)
           .ToLocal(&result)) {
    SetError(error_out, DescribeException(realm->isolate, context, caught));
    return 0;
  }
  return 1;
}

extern "C" int gossamer_v8_realm_drain_microtasks(gossamer_v8_realm *realm,
                                                  const gossamer_v8_host *host,
                                                  char **error_out) {
  if (!RequireRealm(realm, error_out))
    return 0;
  std::lock_guard<std::mutex> guard(realm->mutex);
  if (!RequireRealm(realm, error_out))
    return 0;

  v8::Locker locker(realm->isolate);
  v8::Isolate::Scope isolate_scope(realm->isolate);
  v8::HandleScope handle_scope(realm->isolate);
  v8::Local<v8::Context> context = realm->context.Get(realm->isolate);
  v8::Context::Scope context_scope(context);
  HostScope host_scope(realm, host);
  std::string binding_error;
  if (!EnsureDocumentBinding(realm, context, &binding_error)) {
    SetError(error_out, binding_error);
    return 0;
  }
  realm->isolate->PerformMicrotaskCheckpoint();
  realm->microtask_checkpoints.fetch_add(1, std::memory_order_relaxed);
  while (v8::platform::PumpMessageLoop(
      runtime_platform.get(), realm->isolate,
      v8::platform::MessageLoopBehavior::kDoNotWait)) {
    realm->foreground_tasks.fetch_add(1, std::memory_order_relaxed);
  }
  return 1;
}

extern "C" int gossamer_v8_realm_collect_garbage(gossamer_v8_realm *realm,
                                                 char **error_out) {
  if (!RequireRealm(realm, error_out))
    return 0;
  std::lock_guard<std::mutex> guard(realm->mutex);
  if (!RequireRealm(realm, error_out))
    return 0;

  v8::Locker locker(realm->isolate);
  v8::Isolate::Scope isolate_scope(realm->isolate);
  realm->isolate->LowMemoryNotification();
  return 1;
}

extern "C" size_t gossamer_v8_realm_take_collected_wrappers(
    gossamer_v8_realm *realm, gossamer_v8_node_handle *handles_out,
    size_t capacity) {
  if (realm == nullptr)
    return 0;
  std::lock_guard<std::mutex> guard(realm->mutex);
  if (handles_out == nullptr || capacity == 0)
    return realm->collected_wrappers.size();
  size_t count = std::min(capacity, realm->collected_wrappers.size());
  for (size_t index = 0; index < count; ++index) {
    handles_out[index].document = realm->collected_wrappers[index].document;
    handles_out[index].node = realm->collected_wrappers[index].node;
  }
  realm->collected_wrappers.erase(realm->collected_wrappers.begin(),
                                  realm->collected_wrappers.begin() + count);
  return count;
}

extern "C" int gossamer_v8_realm_profile(gossamer_v8_realm *realm,
                                         gossamer_v8_profile *profile_out,
                                         char **error_out) {
  if (profile_out == nullptr) {
    SetError(error_out, "V8 profile output is null");
    return 0;
  }
  if (!RequireRealm(realm, error_out))
    return 0;
  std::lock_guard<std::mutex> guard(realm->mutex);
  if (!RequireRealm(realm, error_out))
    return 0;

  std::memset(profile_out, 0, sizeof(*profile_out));
  v8::Locker locker(realm->isolate);
  v8::Isolate::Scope isolate_scope(realm->isolate);
  v8::HandleScope handle_scope(realm->isolate);
  FillHeapStatistics(realm->isolate, &profile_out->heap);

  if (realm->sampling) {
    std::unique_ptr<v8::AllocationProfile> profile(
        realm->isolate->GetHeapProfiler()->GetAllocationProfile());
    if (profile) {
      for (const auto &sample : profile->GetSamples()) {
        ++profile_out->samples;
        profile_out->sampled_bytes += sample.size * sample.count;
        if (sample.is_live) {
          ++profile_out->live_samples;
          profile_out->live_bytes += sample.size * sample.count;
        }
      }
    }
  }

  profile_out->evaluations = realm->evaluations.load(std::memory_order_relaxed);
  profile_out->evaluation_nanos =
      realm->evaluation_nanos.load(std::memory_order_relaxed);
  profile_out->microtask_checkpoints =
      realm->microtask_checkpoints.load(std::memory_order_relaxed);
  profile_out->foreground_tasks =
      realm->foreground_tasks.load(std::memory_order_relaxed);
  profile_out->gc_prologues =
      realm->gc_prologues.load(std::memory_order_relaxed);
  profile_out->gc_epilogues =
      realm->gc_epilogues.load(std::memory_order_relaxed);
  profile_out->minor_gcs = realm->minor_gcs.load(std::memory_order_relaxed);
  profile_out->major_gcs = realm->major_gcs.load(std::memory_order_relaxed);
  profile_out->gc_nanos = realm->gc_nanos.load(std::memory_order_relaxed);
  profile_out->wrappers_created =
      realm->wrappers_created.load(std::memory_order_relaxed);
  profile_out->wrapper_cache_hits =
      realm->wrapper_cache_hits.load(std::memory_order_relaxed);
  profile_out->wrappers_collected =
      realm->wrappers_collected.load(std::memory_order_relaxed);
  profile_out->live_wrappers = realm->wrappers.size();
  profile_out->callbacks_created =
      realm->callbacks_created.load(std::memory_order_relaxed);
  profile_out->callbacks_invoked =
      realm->callbacks_invoked.load(std::memory_order_relaxed);
  profile_out->live_callbacks = realm->callbacks.size();
  profile_out->event_listeners = realm->event_listener_count;
  profile_out->events_dispatched =
      realm->events_dispatched.load(std::memory_order_relaxed);
  return 1;
}

extern "C" void gossamer_v8_realm_delete(gossamer_v8_realm *realm) {
  if (realm == nullptr)
    return;
  std::unique_lock<std::mutex> guard(realm->mutex);
  if (!realm->closed && realm->isolate != nullptr) {
    {
      v8::Locker locker(realm->isolate);
      v8::Isolate::Scope isolate_scope(realm->isolate);
      realm->isolate->RemoveGCPrologueCallback(GCPrologue, realm);
      realm->isolate->RemoveGCEpilogueCallback(GCEpilogue, realm);
      if (realm->sampling) {
        realm->isolate->GetHeapProfiler()->StopSamplingHeapProfiler();
        realm->sampling = false;
      }
      ClearRealmHandles(realm);
      realm->context.Reset();
      realm->isolate->SetData(0, nullptr);
    }
    v8::platform::NotifyIsolateShutdown(runtime_platform.get(), realm->isolate);
    realm->isolate->Dispose();
    realm->isolate = nullptr;
  }
  realm->closed = true;
  realm->allocator.reset();
  guard.unlock();
  delete realm;
}

extern "C" void gossamer_v8_error_free(char *error) { std::free(error); }
