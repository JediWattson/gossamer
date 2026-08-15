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
  v8::Global<v8::ObjectTemplate> node_template;
  const gossamer_v8_host *active_host = nullptr;
  bool sampling = false;
  bool closed = false;

  std::unordered_map<WrapperKey, WrapperEntry, WrapperKeyHash> wrappers;
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
  if (object.IsEmpty() || object->InternalFieldCount() != 2 || key == nullptr)
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
    realm->wrappers_collected.fetch_add(1, std::memory_order_relaxed);
  }
  delete weak;
}

v8::MaybeLocal<v8::Object>
GetOrCreateNodeWrapper(gossamer_v8_realm *realm, v8::Local<v8::Context> context,
                       const WrapperKey &key) {
  auto cached = realm->wrappers.find(key);
  if (cached != realm->wrappers.end()) {
    realm->wrapper_cache_hits.fetch_add(1, std::memory_order_relaxed);
    return cached->second.object.Get(realm->isolate);
  }

  v8::Local<v8::ObjectTemplate> node_template =
      realm->node_template.Get(realm->isolate);
  v8::Local<v8::Object> object;
  if (!node_template->NewInstance(context).ToLocal(&object))
    return {};
  object->SetInternalField(
      kNodeDocumentField,
      v8::BigInt::NewFromUnsigned(realm->isolate, key.document));
  object->SetInternalField(
      kNodeIDField, v8::Integer::NewFromUnsigned(realm->isolate, key.node));

  auto inserted = realm->wrappers.try_emplace(key);
  WrapperEntry &entry = inserted.first->second;
  entry.weak = new WrapperWeakData{realm, key};
  entry.object.Reset(realm->isolate, object);
  entry.object.SetWeak(entry.weak, WrapperCollected,
                       v8::WeakCallbackType::kParameter);
  realm->wrappers_created.fetch_add(1, std::memory_order_relaxed);
  return object;
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
  if (!GetOrCreateNodeWrapper(realm, context, WrapperKey{document, node})
           .ToLocal(&wrapper)) {
    ThrowError(isolate, "V8 failed to allocate a DOM node wrapper");
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
                              WrapperKey{document, node})
           .ToLocal(&wrapper)) {
    ThrowError(isolate, "V8 failed to allocate a DOM node wrapper");
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void DocumentCreateElement(const v8::FunctionCallbackInfo<v8::Value> &info) {
  DocumentCreateNode(info, true);
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

bool InstallBindings(gossamer_v8_realm *realm, v8::Local<v8::Context> context) {
  v8::Isolate *isolate = realm->isolate;
  v8::Local<v8::ObjectTemplate> node_template =
      v8::ObjectTemplate::New(isolate);
  node_template->SetInternalFieldCount(2);
  node_template->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "textContent"),
      NodeTextContentGetter, NodeTextContentSetter);
  node_template->Set(isolate, "appendChild",
                     v8::FunctionTemplate::New(isolate, NodeAppendChild));
  node_template->Set(isolate, "insertBefore",
                     v8::FunctionTemplate::New(isolate, NodeInsertBefore));
  node_template->Set(isolate, "removeChild",
                     v8::FunctionTemplate::New(isolate, NodeRemoveChild));
  node_template->Set(isolate, "getAttribute",
                     v8::FunctionTemplate::New(isolate, NodeGetAttribute));
  node_template->Set(isolate, "setAttribute",
                     v8::FunctionTemplate::New(isolate, NodeSetAttribute));
  node_template->Set(isolate, "removeAttribute",
                     v8::FunctionTemplate::New(isolate, NodeRemoveAttribute));
  node_template->Set(isolate, "addEventListener",
                     v8::FunctionTemplate::New(isolate, NodeAddEventListener));
  node_template->Set(
      isolate, "removeEventListener",
      v8::FunctionTemplate::New(isolate, NodeRemoveEventListener));
  realm->node_template.Reset(isolate, node_template);

  v8::Local<v8::Object> document = v8::Object::New(isolate);
  v8::Local<v8::Function> get_element_by_id;
  v8::Local<v8::Function> create_element;
  v8::Local<v8::Function> create_text_node;
  v8::Local<v8::Function> queue_microtask;
  v8::Local<v8::Function> set_timeout;
  v8::Local<v8::Function> clear_timeout;
  if (!v8::Function::New(context, DocumentGetElementByID)
           .ToLocal(&get_element_by_id) ||
      !v8::Function::New(context, DocumentCreateElement)
           .ToLocal(&create_element) ||
      !v8::Function::New(context, DocumentCreateTextNode)
           .ToLocal(&create_text_node) ||
      !v8::Function::New(context, QueueMicrotaskCallback)
           .ToLocal(&queue_microtask) ||
      !v8::Function::New(context, SetTimeoutCallback).ToLocal(&set_timeout) ||
      !v8::Function::New(context, ClearTimeoutCallback)
           .ToLocal(&clear_timeout)) {
    return false;
  }
  v8::Local<v8::Object> global = context->Global();
  return document
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "getElementById"),
                   get_element_by_id)
             .FromMaybe(false) &&
         document
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "createElement"),
                   create_element)
             .FromMaybe(false) &&
         document
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "createTextNode"),
                   create_text_node)
             .FromMaybe(false) &&
         global
             ->Set(context, v8::String::NewFromUtf8Literal(isolate, "document"),
                   document)
             .FromMaybe(false) &&
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
  realm->node_template.Reset();
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
