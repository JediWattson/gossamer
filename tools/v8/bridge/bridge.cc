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
#include <utility>
#include <vector>

#include "include/libplatform/libplatform.h"
#include "include/v8-array-buffer.h"
#include "include/v8-context.h"
#include "include/v8-container.h"
#include "include/v8-exception.h"
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

constexpr uint8_t kClickEvent = 1;
constexpr int kNodeDocumentField = 0;
constexpr int kNodeIDField = 1;
constexpr int kNodeStyleField = 2;
constexpr int kStyleNodeField = 0;
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
  uint8_t type = 0;

  bool operator==(const ListenerKey &other) const {
    return target == other.target && type == other.type;
  }
};

struct ListenerKeyHash {
  size_t operator()(const ListenerKey &key) const {
    size_t target = WrapperKeyHash{}(key.target);
    size_t type = std::hash<uint8_t>{}(key.type);
    return target ^ (type + 0x9e3779b9U + (target << 6U) + (target >> 2U));
  }
};

struct WrapperWeakData;

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
  v8::Global<v8::ObjectTemplate> style_template;
  v8::Global<v8::Object> document_wrapper;
  const gossamer_v8_host *active_host = nullptr;
  bool sampling = false;
  bool closed = false;
  bool document_bound = false;
  WrapperKey document_key;
  std::string base_uri;

  std::unordered_map<WrapperKey, WrapperEntry, WrapperKeyHash> wrappers;
  std::vector<WrapperKey> collected_wrappers;
  std::unordered_map<ListenerKey, std::vector<v8::Global<v8::Function>>,
                     ListenerKeyHash>
      listeners;
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
                        uint8_t *event_type) {
  if (!value->IsString()) {
    ThrowError(isolate, "event type must be a string");
    return false;
  }
  std::string name = UTF8Value(isolate, value);
  if (name != "click") {
    ThrowError(isolate,
               "only click event listeners are supported in this milestone");
    return false;
  }
  *event_type = kClickEvent;
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

void NodeChildrenGetter(v8::Local<v8::Name> property,
                        const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  bool elements_only =
      UTF8Value(isolate, property.As<v8::Value>()) == "children";
  std::vector<uint32_t> nodes;
  std::string error;
  if (!ReadChildNodes(realm, key, elements_only, &nodes, &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (nodes.size() > static_cast<size_t>(std::numeric_limits<int>::max())) {
    ThrowError(isolate, "DOM child collection exceeds V8's array limit");
    return;
  }
  v8::Local<v8::Array> result =
      v8::Array::New(isolate, static_cast<int>(nodes.size()));
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  for (size_t index = 0; index < nodes.size(); ++index) {
    v8::Local<v8::Object> wrapper;
    if (!GetOrCreateNodeWrapper(realm, context,
                                WrapperKey{key.document, nodes[index]}, &error)
             .ToLocal(&wrapper) ||
        !result->Set(context, static_cast<uint32_t>(index), wrapper)
             .FromMaybe(false)) {
      ThrowError(isolate, error.empty() ? "V8 failed to build a child collection"
                                        : error);
      return;
    }
  }
  info.GetReturnValue().Set(result);
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
  const char *attribute = property_name == "className" ? "class" : "id";
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
          realm->active_host->execution_id, key.document, key.node, attribute,
          std::strlen(attribute), &value, &value_length, &found,
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
  const char *attribute = property_name == "className" ? "class" : "id";
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->set_attribute(
          realm->active_host->execution_id, key.document, key.node, attribute,
          std::strlen(attribute), rendered.data(), rendered.size(),
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
  uint8_t event_type = 0;
  if (!EventTypeFromValue(isolate, info[0], &event_type))
    return;
  v8::Local<v8::Function> function = info[1].As<v8::Function>();
  auto &listeners = realm->listeners[ListenerKey{key, event_type}];
  for (const auto &listener : listeners) {
    if (listener == function)
      return;
  }
  listeners.emplace_back(isolate, function);
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
  uint8_t event_type = 0;
  if (!EventTypeFromValue(isolate, info[0], &event_type))
    return;
  auto found = realm->listeners.find(ListenerKey{key, event_type});
  if (found == realm->listeners.end())
    return;
  v8::Local<v8::Function> function = info[1].As<v8::Function>();
  auto &listeners = found->second;
  for (auto listener = listeners.begin(); listener != listeners.end();
       ++listener) {
    if (*listener == function) {
      listener->Reset();
      listeners.erase(listener);
      --realm->event_listener_count;
      break;
    }
  }
  if (listeners.empty())
    realm->listeners.erase(found);
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
  v8::Local<v8::FunctionTemplate> text_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> document_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);

  event_target_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "EventTarget"));
  node_template->SetClassName(v8::String::NewFromUtf8Literal(isolate, "Node"));
  element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "Element"));
  html_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLElement"));
  text_template->SetClassName(v8::String::NewFromUtf8Literal(isolate, "Text"));
  document_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "Document"));
  node_template->Inherit(event_target_template);
  element_template->Inherit(node_template);
  html_element_template->Inherit(element_template);
  text_template->Inherit(node_template);
  document_template->Inherit(node_template);
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {node_template, element_template, html_element_template, text_template,
        document_template}) {
    interface_template->InstanceTemplate()->SetInternalFieldCount(3);
  }

  v8::Local<v8::ObjectTemplate> event_target_prototype =
      event_target_template->PrototypeTemplate();
  event_target_prototype->Set(
      isolate, "addEventListener",
      v8::FunctionTemplate::New(isolate, NodeAddEventListener));
  event_target_prototype->Set(
      isolate, "removeEventListener",
      v8::FunctionTemplate::New(isolate, NodeRemoveEventListener));

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
  element_prototype->Set(isolate, "getAttribute",
                         v8::FunctionTemplate::New(isolate, NodeGetAttribute));
  element_prototype->Set(isolate, "setAttribute",
                         v8::FunctionTemplate::New(isolate, NodeSetAttribute));
  element_prototype->Set(
      isolate, "removeAttribute",
      v8::FunctionTemplate::New(isolate, NodeRemoveAttribute));
  element_prototype->Set(isolate, "hasAttribute",
                         v8::FunctionTemplate::New(isolate, NodeHasAttribute));

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
  document_prototype->Set(
      isolate, "getElementById",
      v8::FunctionTemplate::New(isolate, DocumentGetElementByID));
  document_prototype->Set(
      isolate, "createElement",
      v8::FunctionTemplate::New(isolate, DocumentCreateElement));
  document_prototype->Set(
      isolate, "createElementNS",
      v8::FunctionTemplate::New(isolate, DocumentCreateElementNS));
  document_prototype->Set(
      isolate, "createTextNode",
      v8::FunctionTemplate::New(isolate, DocumentCreateTextNode));

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
      };
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {node_template, element_template, html_element_template, text_template,
        document_template}) {
    install_node_instance_surface(interface_template->InstanceTemplate());
  }
  install_element_instance_surface(element_template->InstanceTemplate());
  install_element_instance_surface(html_element_template->InstanceTemplate());
  text_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "data"), NodeValueGetter,
      NodeValueSetter);
  install_parent_instance_surface(document_template->InstanceTemplate());
  for (const char *name : {"documentElement", "head", "body"}) {
    document_template->InstanceTemplate()->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeRelationGetter);
  }
  document_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "defaultView"),
      DocumentDefaultViewGetter);

  realm->event_target_template.Reset(isolate, event_target_template);
  realm->node_template.Reset(isolate, node_template);
  realm->element_template.Reset(isolate, element_template);
  realm->html_element_template.Reset(isolate, html_element_template);
  realm->text_template.Reset(isolate, text_template);
  realm->document_template.Reset(isolate, document_template);

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
         expose_interface("Node", node_template) &&
         expose_interface("Element", element_template) &&
         expose_interface("HTMLElement", html_element_template) &&
         expose_interface("Text", text_template) &&
         expose_interface("Document", document_template) &&
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
      listener.Reset();
  }
  realm->listeners.clear();
  realm->event_listener_count = 0;
  for (auto &callback : realm->callbacks)
    callback.second.Reset();
  realm->callbacks.clear();
  realm->timer_callbacks.clear();
  realm->callback_timers.clear();
  realm->event_target_template.Reset();
  realm->node_template.Reset();
  realm->element_template.Reset();
  realm->html_element_template.Reset();
  realm->text_template.Reset();
  realm->document_template.Reset();
  realm->style_template.Reset();
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
                                                uint8_t event_type,
                                                uint64_t document,
                                                uint32_t node, double, double,
                                                int32_t, char **error_out) {
  if (!RequireRealm(realm, error_out))
    return 0;
  std::lock_guard<std::mutex> guard(realm->mutex);
  if (!RequireRealm(realm, error_out))
    return 0;
  if (event_type != kClickEvent) {
    SetError(error_out, "V8 received an unsupported browser event type");
    return 0;
  }

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
  auto found = realm->listeners.find(
      ListenerKey{WrapperKey{document, node}, event_type});
  realm->events_dispatched.fetch_add(1, std::memory_order_relaxed);
  if (found == realm->listeners.end())
    return 1;

  for (const auto &listener : found->second) {
    uint64_t callback =
        StoreOneShotCallback(realm, listener.Get(realm->isolate));
    std::string error;
    if (!QueuePublishedCallback(realm, callback, false, &error)) {
      RemoveCallback(realm, callback);
      SetError(error_out, error);
      return 0;
    }
  }
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
