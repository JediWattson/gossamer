#include "bridge.h"

#include <algorithm>
#include <atomic>
#include <chrono>
#include <cmath>
#include <cstdlib>
#include <cstring>
#include <functional>
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
#include "include/v8-json.h"
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
constexpr int kNodeFormCollectionField = 7;
constexpr int kNodeInternalFieldCount = 8;
constexpr int kStyleNodeField = 0;
constexpr int kStyleComputedField = 1;
constexpr int kStylePseudoField = 2;
constexpr int kStyleInternalFieldCount = 3;
constexpr int kFacadeNodeField = 0;
constexpr int kFacadeBackingField = 1;
constexpr int kFacadeInternalFieldCount = 2;
constexpr int kIteratorFacadeField = 0;
constexpr int kIteratorIndexField = 1;
constexpr int kIteratorSourceKindField = 2;
constexpr int kIteratorModeField = 3;
constexpr int kEventStateField = 0;
constexpr int kMutationObserverStateField = 0;
constexpr int kLayoutObserverStateField = 0;
constexpr int kRangeStateField = 0;
constexpr int kTraversalStateField = 0;
constexpr int kSelectionStateField = 0;
constexpr int kFormDataEntriesField = 0;
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
  CustomEvent,
  MouseEvent,
  PointerEvent,
  KeyboardEvent,
  InputEvent,
  CompositionEvent,
  FocusEvent,
};

enum class FacadeKind : int32_t {
  NodeList = 1,
  HTMLCollection = 2,
  ClassList = 3,
  SelectOptions = 4,
  FormElements = 5,
};

enum class IteratorMode : int32_t {
  Keys = 1,
  Values = 2,
  Entries = 3,
};

enum class DOMMutationOperation : uint8_t {
  Append = 1,
  Prepend = 2,
  Before = 3,
  After = 4,
  ReplaceWith = 5,
  ReplaceChildren = 6,
  Remove = 7,
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
  bool persisted = false;
};

struct WrapperWeakData;
struct EventWeakData;
struct MutationObserverWeakData;
struct LayoutObserverWeakData;
struct RangeWeakData;
struct TraversalWeakData;

struct MutationObserverOptions {
  bool child_list = false;
  bool attributes = false;
  bool character_data = false;
  bool subtree = false;
  bool attribute_old_value = false;
  bool character_data_old_value = false;
  std::unordered_set<std::string> attribute_filter;
};

struct MutationObserverRegistration {
  WrapperKey target;
  MutationObserverOptions options;
  v8::Global<v8::Object> target_object;
};

struct ObserverMutationRecord {
  uint64_t sequence = 0;
  uint8_t type = 0;
  WrapperKey target;
  std::vector<uint32_t> added_nodes;
  std::vector<uint32_t> removed_nodes;
  uint32_t previous_sibling = 0;
  bool has_previous_sibling = false;
  uint32_t next_sibling = 0;
  bool has_next_sibling = false;
  std::string attribute_name;
  std::string old_value;
  bool expose_old_value = false;
};

struct MutationObserverState {
  gossamer_v8_realm *realm = nullptr;
  MutationObserverWeakData *weak = nullptr;
  v8::Global<v8::Function> callback;
  std::vector<MutationObserverRegistration> registrations;
  std::vector<ObserverMutationRecord> records;
  uint64_t cursor = 0;
};

enum class LayoutObserverKind : uint8_t { Resize, Intersection };

struct LayoutObserverRegistration {
  WrapperKey target;
  v8::Global<v8::Object> target_object;
  bool has_last = false;
  double last_width = 0;
  double last_height = 0;
  double last_ratio = 0;
  bool last_intersecting = false;
};

struct LayoutObserverState {
  gossamer_v8_realm *realm = nullptr;
  LayoutObserverWeakData *weak = nullptr;
  LayoutObserverKind kind = LayoutObserverKind::Resize;
  v8::Global<v8::Function> callback;
  std::vector<LayoutObserverRegistration> registrations;
  std::vector<double> thresholds{0};
};

struct RangeState {
  gossamer_v8_realm *realm = nullptr;
  RangeWeakData *weak = nullptr;
  WrapperKey start;
  WrapperKey end;
  uint32_t start_offset = 0;
  uint32_t end_offset = 0;
  v8::Global<v8::Object> start_object;
  v8::Global<v8::Object> end_object;
};

struct TraversalState {
  gossamer_v8_realm *realm = nullptr;
  TraversalWeakData *weak = nullptr;
  WrapperKey root;
  std::vector<WrapperKey> nodes;
  size_t index = 0;
  bool node_iterator = false;
  bool root_accepted = true;
  uint32_t what_to_show = 0xffffffffu;
  bool pointer_before_reference = true;
  uint64_t mutation_sequence = 0;
  v8::Global<v8::Object> root_object;
  v8::Global<v8::Value> filter;
};

struct SelectionState {
  gossamer_v8_realm *realm = nullptr;
  v8::Global<v8::Object> range_object;
};

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
  v8::Global<v8::FunctionTemplate> html_form_element_template;
  v8::Global<v8::FunctionTemplate> html_input_element_template;
  v8::Global<v8::FunctionTemplate> html_text_area_element_template;
  v8::Global<v8::FunctionTemplate> html_select_element_template;
  v8::Global<v8::FunctionTemplate> html_option_element_template;
  v8::Global<v8::FunctionTemplate> html_button_element_template;
  v8::Global<v8::FunctionTemplate> html_template_element_template;
  v8::Global<v8::FunctionTemplate> html_iframe_element_template;
  v8::Global<v8::FunctionTemplate> html_head_element_template;
  v8::Global<v8::FunctionTemplate> html_script_element_template;
  v8::Global<v8::FunctionTemplate> html_media_element_template;
  v8::Global<v8::FunctionTemplate> html_image_element_template;
  v8::Global<v8::FunctionTemplate> form_data_template;
  v8::Global<v8::FunctionTemplate> text_template;
  v8::Global<v8::FunctionTemplate> document_template;
  v8::Global<v8::FunctionTemplate> document_fragment_template;
  v8::Global<v8::FunctionTemplate> history_template;
  v8::Global<v8::FunctionTemplate> location_template;
  v8::Global<v8::FunctionTemplate> event_template;
  v8::Global<v8::FunctionTemplate> custom_event_template;
  v8::Global<v8::FunctionTemplate> mouse_event_template;
  v8::Global<v8::FunctionTemplate> pointer_event_template;
  v8::Global<v8::FunctionTemplate> keyboard_event_template;
  v8::Global<v8::FunctionTemplate> input_event_template;
  v8::Global<v8::FunctionTemplate> composition_event_template;
  v8::Global<v8::FunctionTemplate> focus_event_template;
  v8::Global<v8::FunctionTemplate> mutation_observer_template;
  v8::Global<v8::FunctionTemplate> mutation_record_template;
  v8::Global<v8::FunctionTemplate> resize_observer_template;
  v8::Global<v8::FunctionTemplate> intersection_observer_template;
  v8::Global<v8::FunctionTemplate> range_template;
  v8::Global<v8::FunctionTemplate> tree_walker_template;
  v8::Global<v8::FunctionTemplate> node_iterator_template;
  v8::Global<v8::FunctionTemplate> selection_template;
  v8::Global<v8::FunctionTemplate> dom_rect_template;
  v8::Global<v8::FunctionTemplate> node_list_template;
  v8::Global<v8::FunctionTemplate> html_collection_template;
  v8::Global<v8::FunctionTemplate> token_list_template;
  v8::Global<v8::FunctionTemplate> dataset_template;
  v8::Global<v8::FunctionTemplate> style_template;
  v8::Global<v8::ObjectTemplate> collection_iterator_template;
  v8::Global<v8::Object> document_wrapper;
  v8::Global<v8::Object> history_object;
  v8::Global<v8::Object> location_object;
  v8::Global<v8::Object> selection_object;
  SelectionState *selection_state = nullptr;
  const gossamer_v8_host *active_host = nullptr;
  bool sampling = false;
  bool closed = false;
  bool document_bound = false;
  WrapperKey document_key;
  std::string base_uri;
  std::string scroll_restoration = "auto";

  std::unordered_map<WrapperKey, WrapperEntry, WrapperKeyHash> wrappers;
  std::vector<WrapperKey> collected_wrappers;
  std::unordered_map<ListenerKey, std::vector<std::unique_ptr<ListenerRecord>>,
                     ListenerKeyHash>
      listeners;
  std::unordered_set<EventWeakData *> events;
  std::unordered_set<MutationObserverWeakData *> mutation_observers;
  std::unordered_set<LayoutObserverWeakData *> layout_observers;
  std::unordered_set<RangeWeakData *> ranges;
  std::unordered_set<TraversalWeakData *> traversals;
  uint64_t next_listener = 1;
  uint32_t dispatch_depth = 0;
  uint64_t event_listener_count = 0;
  uint64_t next_callback = 1;
  std::unordered_map<uint64_t, v8::Global<v8::Function>> callbacks;
  std::unordered_map<uint64_t, uint64_t> timer_callbacks;
  std::unordered_map<uint64_t, uint64_t> callback_timers;
  std::unordered_map<uint64_t, uint64_t> animation_frame_callbacks;
  std::unordered_map<uint64_t, uint64_t> callback_animation_frames;
  std::unordered_map<std::string, v8::Global<v8::Module>> modules;
  std::unordered_map<std::string, std::string> module_resolutions;

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

struct MutationObserverWeakData {
  gossamer_v8_realm *realm;
  MutationObserverState *state;
  v8::Global<v8::Object> object;
};

struct LayoutObserverWeakData {
  gossamer_v8_realm *realm;
  LayoutObserverState *state;
  v8::Global<v8::Object> object;
};

struct RangeWeakData {
  gossamer_v8_realm *realm;
  RangeState *state;
  v8::Global<v8::Object> object;
};

struct TraversalWeakData {
  gossamer_v8_realm *realm;
  TraversalState *state;
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

constexpr const char *kDOMExceptionPrefix = "__GOSSAMER_DOM_EXCEPTION__:";

int DOMExceptionLegacyCode(const std::string &name) {
  if (name == "IndexSizeError")
    return 1;
  if (name == "HierarchyRequestError")
    return 3;
  if (name == "InvalidCharacterError")
    return 5;
  if (name == "NotFoundError")
    return 8;
  if (name == "NotSupportedError")
    return 9;
  if (name == "NamespaceError")
    return 14;
  if (name == "InvalidStateError")
    return 11;
  if (name == "SyntaxError")
    return 12;
  if (name == "InvalidNodeTypeError")
    return 24;
  return 0;
}

void InitializeDOMException(v8::Isolate *isolate,
                            v8::Local<v8::Object> object,
                            const std::string &message,
                            const std::string &name) {
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::String> rendered_message;
  v8::Local<v8::String> rendered_name;
  if (!v8::String::NewFromUtf8(isolate, message.data(),
                               v8::NewStringType::kNormal,
                               static_cast<int>(std::min<size_t>(
                                   message.size(),
                                   std::numeric_limits<int>::max())))
           .ToLocal(&rendered_message))
    rendered_message = v8::String::Empty(isolate);
  if (!v8::String::NewFromUtf8(isolate, name.data(),
                               v8::NewStringType::kNormal,
                               static_cast<int>(std::min<size_t>(
                                   name.size(),
                                   std::numeric_limits<int>::max())))
           .ToLocal(&rendered_name))
    rendered_name = v8::String::NewFromUtf8Literal(isolate, "Error");
  object
      ->DefineOwnProperty(context,
                          v8::String::NewFromUtf8Literal(isolate, "message"),
                          rendered_message,
                          static_cast<v8::PropertyAttribute>(v8::ReadOnly |
                                                             v8::DontEnum))
      .FromMaybe(false);
  object
      ->DefineOwnProperty(context,
                          v8::String::NewFromUtf8Literal(isolate, "name"),
                          rendered_name,
                          static_cast<v8::PropertyAttribute>(v8::ReadOnly |
                                                             v8::DontEnum))
      .FromMaybe(false);
  object
      ->DefineOwnProperty(
          context, v8::String::NewFromUtf8Literal(isolate, "code"),
          v8::Integer::New(isolate, DOMExceptionLegacyCode(name)),
          static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontEnum))
      .FromMaybe(false);
}

void DOMExceptionConstructor(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  if (!info.IsConstructCall()) {
    isolate->ThrowException(v8::Exception::TypeError(
        v8::String::NewFromUtf8Literal(isolate,
                                       "DOMException constructor requires new")));
    return;
  }
  std::string message;
  std::string name = "Error";
  if (info.Length() > 0 && !info[0]->IsUndefined()) {
    v8::String::Utf8Value rendered(isolate, info[0]);
    if (*rendered != nullptr)
      message.assign(*rendered, rendered.length());
  }
  if (info.Length() > 1 && !info[1]->IsUndefined()) {
    v8::String::Utf8Value rendered(isolate, info[1]);
    if (*rendered != nullptr)
      name.assign(*rendered, rendered.length());
  }
  InitializeDOMException(isolate, info.This(), message, name);
  info.GetReturnValue().Set(info.This());
}

void DOMExceptionToString(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Value> name;
  v8::Local<v8::Value> message;
  if (!info.This()
           ->Get(context, v8::String::NewFromUtf8Literal(isolate, "name"))
           .ToLocal(&name) ||
      !info.This()
           ->Get(context,
                 v8::String::NewFromUtf8Literal(isolate, "message"))
           .ToLocal(&message))
    return;
  std::string rendered_name = UTF8Value(isolate, name);
  std::string rendered_message = UTF8Value(isolate, message);
  std::string result = rendered_name;
  if (!rendered_message.empty())
    result += ": " + rendered_message;
  v8::Local<v8::String> rendered;
  if (v8::String::NewFromUtf8(isolate, result.data(),
                              v8::NewStringType::kNormal,
                              static_cast<int>(result.size()))
          .ToLocal(&rendered))
    info.GetReturnValue().Set(rendered);
}

void ThrowError(v8::Isolate *isolate, const std::string &message) {
  const size_t prefix_length = std::strlen(kDOMExceptionPrefix);
  if (message.compare(0, prefix_length, kDOMExceptionPrefix) == 0) {
    size_t separator = message.find(':', prefix_length);
    if (separator != std::string::npos) {
      std::string name = message.substr(prefix_length,
                                        separator - prefix_length);
      std::string detail = message.substr(separator + 1);
      v8::Local<v8::Context> context = isolate->GetCurrentContext();
      v8::Local<v8::Value> constructor_value;
      if (context->Global()
              ->Get(context,
                    v8::String::NewFromUtf8Literal(isolate, "DOMException"))
              .ToLocal(&constructor_value) &&
          constructor_value->IsFunction()) {
        v8::Local<v8::String> rendered_detail;
        v8::Local<v8::String> rendered_name;
        if (v8::String::NewFromUtf8(isolate, detail.data(),
                                    v8::NewStringType::kNormal,
                                    static_cast<int>(detail.size()))
                    .ToLocal(&rendered_detail) &&
            v8::String::NewFromUtf8(isolate, name.data(),
                                    v8::NewStringType::kNormal,
                                    static_cast<int>(name.size()))
                    .ToLocal(&rendered_name)) {
          v8::Local<v8::Value> arguments[] = {rendered_detail, rendered_name};
          v8::Local<v8::Object> exception;
          if (constructor_value.As<v8::Function>()
                  ->NewInstance(context, 2, arguments)
                  .ToLocal(&exception)) {
            isolate->ThrowException(exception);
            return;
          }
        }
      }
    }
  }
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

void ThrowDOMException(v8::Isolate *isolate, const std::string &name,
                       const std::string &message) {
  ThrowError(isolate, std::string(kDOMExceptionPrefix) + name + ":" + message);
}

std::string ModuleResolutionKey(const std::string &referrer,
                                const std::string &specifier) {
  return std::to_string(referrer.size()) + ":" + referrer + specifier;
}

v8::MaybeLocal<v8::Module>
ResolveModule(v8::Local<v8::Context> context,
              v8::Local<v8::String> specifier, v8::Local<v8::FixedArray>,
              v8::Local<v8::Module> referrer) {
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string referrer_url = UTF8Value(isolate, referrer->GetResourceName());
  std::string requested = UTF8Value(isolate, specifier);
  auto resolution = realm->module_resolutions.find(
      ModuleResolutionKey(referrer_url, requested));
  if (resolution == realm->module_resolutions.end()) {
    ThrowError(isolate, "unresolved module specifier \"" + requested +
                            "\" from \"" + referrer_url + "\"");
    return {};
  }
  auto module = realm->modules.find(resolution->second);
  if (module == realm->modules.end()) {
    ThrowError(isolate, "module source was not compiled for \"" +
                            resolution->second + "\"");
    return {};
  }
  return module->second.Get(isolate);
}

v8::MaybeLocal<v8::Promise> ImportModuleDynamically(
    v8::Local<v8::Context> context, v8::Local<v8::Data>,
    v8::Local<v8::Value> resource_name, v8::Local<v8::String> specifier,
    v8::ModuleImportPhase phase, v8::Local<v8::FixedArray>) {
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  v8::Local<v8::Promise::Resolver> resolver;
  if (!v8::Promise::Resolver::New(context).ToLocal(&resolver))
    return {};
  auto reject = [&](v8::Local<v8::Value> reason) {
    resolver->Reject(context, reason).FromMaybe(false);
    return resolver->GetPromise();
  };
  if (phase != v8::ModuleImportPhase::kEvaluation) {
    return reject(v8::Exception::TypeError(v8::String::NewFromUtf8Literal(
        isolate, "source-phase dynamic import is unsupported")));
  }
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string referrer = UTF8Value(isolate, resource_name);
  std::string requested = UTF8Value(isolate, specifier);
  auto resolution = realm->module_resolutions.find(
      ModuleResolutionKey(referrer, requested));
  if (resolution == realm->module_resolutions.end()) {
    return reject(v8::Exception::TypeError(v8::String::NewFromUtf8Literal(
        isolate, "unresolved dynamic module specifier")));
  }
  auto target = realm->modules.find(resolution->second);
  if (target == realm->modules.end()) {
    return reject(v8::Exception::TypeError(v8::String::NewFromUtf8Literal(
        isolate, "dynamic module source was not compiled")));
  }
  v8::TryCatch caught(isolate);
  v8::Local<v8::Module> module = target->second.Get(isolate);
  bool ok = true;
  if (module->GetStatus() == v8::Module::kUninstantiated)
    ok = module->InstantiateModule(context, ResolveModule).FromMaybe(false);
  if (ok && module->GetStatus() == v8::Module::kInstantiated) {
    v8::Local<v8::Value> result;
    ok = module->Evaluate(context).ToLocal(&result);
  }
  if (ok && module->GetStatus() == v8::Module::kErrored) {
    return reject(module->GetException());
  }
  if (!ok) {
    v8::Local<v8::Value> reason = caught.HasCaught()
                                      ? caught.Exception()
                                      : v8::Exception::Error(
                                            v8::String::NewFromUtf8Literal(
                                                isolate,
                                                "dynamic module evaluation failed"));
    caught.Reset();
    return reject(reason);
  }
  resolver->Resolve(context, module->GetModuleNamespace()).FromMaybe(false);
  return resolver->GetPromise();
}

void ThrowTypeError(v8::Isolate *isolate, const std::string &message) {
  v8::Local<v8::String> rendered;
  if (!v8::String::NewFromUtf8(
           isolate, message.data(), v8::NewStringType::kNormal,
           static_cast<int>(std::min<size_t>(message.size(),
                                             std::numeric_limits<int>::max())))
           .ToLocal(&rendered)) {
    rendered = v8::String::NewFromUtf8Literal(isolate, "invalid operation");
  }
  isolate->ThrowException(v8::Exception::TypeError(rendered));
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

struct StyleReference {
  WrapperKey key;
  bool computed = false;
  std::string pseudo;
};

bool ReadStyleReference(v8::Isolate *isolate,
                        v8::Local<v8::Object> receiver,
                        StyleReference *reference) {
  if (receiver.IsEmpty() ||
      receiver->InternalFieldCount() != kStyleInternalFieldCount ||
      reference == nullptr) {
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
      !ReadWrapperKey(node_value.As<v8::Object>(), &reference->key)) {
    ThrowError(isolate, "Gossamer style declaration lost its element");
    return false;
  }
  v8::Local<v8::Data> computed_data =
      receiver->GetInternalField(kStyleComputedField);
  v8::Local<v8::Data> pseudo_data =
      receiver->GetInternalField(kStylePseudoField);
  if (!computed_data->IsValue() || !pseudo_data->IsValue()) {
    ThrowError(isolate, "Gossamer style declaration lost its state");
    return false;
  }
  v8::Local<v8::Value> computed_value = computed_data.As<v8::Value>();
  v8::Local<v8::Value> pseudo_value = pseudo_data.As<v8::Value>();
  if (!computed_value->IsBoolean() || !pseudo_value->IsString()) {
    ThrowError(isolate, "Gossamer style declaration has invalid state");
    return false;
  }
  reference->computed = computed_value->BooleanValue(isolate);
  reference->pseudo = UTF8Value(isolate, pseudo_value);
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

bool IsDashedStylePropertyName(const std::string &name) {
  return name.find('-') != std::string::npos;
}

bool IsSupportedDashedStylePropertyName(const std::string &name) {
  for (const char *supported : {
           "align-content",       "align-items",         "align-self",
           "background-color",    "border-collapse",     "border-spacing",
           "box-sizing",          "caption-side",        "column-gap",
           "empty-cells",
           "flex-basis",          "flex-direction",      "flex-grow",
           "flex-shrink",         "font-family",         "font-size",
           "font-style",          "font-weight",
           "line-height",         "text-decoration",     "text-decoration-line",
           "text-align",          "text-orientation",    "vertical-align",
           "white-space",
           "writing-mode",
           "min-height",          "min-width",           "max-height",
           "max-width",           "table-layout",
           "padding-top",         "padding-right",       "padding-bottom",
           "padding-left",        "margin-top",          "margin-right",
           "margin-bottom",       "margin-left",         "border-top",
           "border-right",        "border-bottom",       "border-left",
           "border-width",        "border-style",        "border-color",
           "border-top-width",    "border-right-width",  "border-bottom-width",
           "border-left-width",   "border-top-style",    "border-right-style",
           "border-bottom-style", "border-left-style",   "border-top-color",
           "border-right-color",  "border-bottom-color", "border-left-color",
           "list-style",          "list-style-type",      "overflow-x",
           "grid-area",           "grid-auto-columns",   "grid-auto-flow",
           "grid-auto-rows",      "grid-column",         "grid-column-end",
           "grid-column-start",   "grid-row",            "grid-row-end",
           "grid-row-start",      "grid-template-areas", "grid-template-columns",
           "grid-template-rows",  "justify-content",     "justify-items",
           "justify-self",        "overflow-y",          "row-gap",
           "z-index",
       }) {
    if (name == supported)
      return true;
  }
  return false;
}

bool IsASCIILetter(unsigned char character) {
  return (character >= 'a' && character <= 'z') ||
         (character >= 'A' && character <= 'Z');
}

bool IsCSSNameStart(unsigned char character) {
  return IsASCIILetter(character) || character == '_' || character >= 0x80;
}

bool IsCSSNameCharacter(unsigned char character) {
  return IsCSSNameStart(character) ||
         (character >= '0' && character <= '9') || character == '-';
}

std::string ASCIILower(const std::string &source) {
  std::string result = source;
  for (char &character : result) {
    if (character >= 'A' && character <= 'Z')
      character = static_cast<char>(character + ('a' - 'A'));
  }
  return result;
}

// This slice intentionally accepts only simple pseudo-element selectors. It
// recognizes the four legacy single-colon spellings but does not attempt to
// embed a selector parser in the V8 bridge. Functional ::part() and
// ::slotted() are called out separately because CSSOM requires their rejection
// even when a future parser accepts other functional pseudo-elements.
bool ValidateComputedStylePseudo(const std::string &pseudo,
                                 std::string *error) {
  if (pseudo.empty())
    return true;

  size_t name_start = 0;
  bool legacy = false;
  if (pseudo.size() >= 2 && pseudo[0] == ':' && pseudo[1] == ':') {
    name_start = 2;
  } else if (pseudo[0] == ':') {
    legacy = true;
    name_start = 1;
  } else {
    *error = "getComputedStyle pseudo-element must be empty or a valid simple "
             "pseudo-element selector";
    return false;
  }

  if (name_start >= pseudo.size()) {
    *error = "getComputedStyle pseudo-element must be empty or a valid simple "
             "pseudo-element selector";
    return false;
  }
  size_t index = name_start;
  unsigned char first = static_cast<unsigned char>(pseudo[index]);
  if (first == '-') {
    ++index;
    if (index >= pseudo.size()) {
      *error = "getComputedStyle pseudo-element must be empty or a valid simple "
               "pseudo-element selector";
      return false;
    }
    unsigned char second = static_cast<unsigned char>(pseudo[index]);
    if (second != '-' && !IsCSSNameStart(second)) {
      *error = "getComputedStyle pseudo-element must be empty or a valid simple "
               "pseudo-element selector";
      return false;
    }
  } else if (!IsCSSNameStart(first)) {
    *error = "getComputedStyle pseudo-element must be empty or a valid simple "
             "pseudo-element selector";
    return false;
  }
  while (index < pseudo.size() &&
         IsCSSNameCharacter(static_cast<unsigned char>(pseudo[index]))) {
    ++index;
  }

  std::string name = ASCIILower(pseudo.substr(name_start, index - name_start));
  if (name == "part" || name == "slotted") {
    *error = "getComputedStyle does not support ::part() or ::slotted()";
    return false;
  }
  if (index != pseudo.size()) {
    *error = "getComputedStyle pseudo-element must be empty or a valid simple "
             "pseudo-element selector";
    return false;
  }
  if (legacy && name != "before" && name != "after" &&
      name != "first-line" && name != "first-letter") {
    *error = "getComputedStyle pseudo-element must be empty or a valid simple "
             "pseudo-element selector";
    return false;
  }
  return true;
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
    if (metadata.namespace_uri != "http://www.w3.org/1999/xhtml") {
      node_template = realm->element_template.Get(realm->isolate);
    } else if (metadata.local_name == "form") {
      node_template = realm->html_form_element_template.Get(realm->isolate);
    } else if (metadata.local_name == "input") {
      node_template = realm->html_input_element_template.Get(realm->isolate);
    } else if (metadata.local_name == "textarea") {
      node_template =
          realm->html_text_area_element_template.Get(realm->isolate);
    } else if (metadata.local_name == "select") {
      node_template = realm->html_select_element_template.Get(realm->isolate);
    } else if (metadata.local_name == "option") {
      node_template = realm->html_option_element_template.Get(realm->isolate);
    } else if (metadata.local_name == "button") {
      node_template = realm->html_button_element_template.Get(realm->isolate);
    } else if (metadata.local_name == "template") {
      node_template =
          realm->html_template_element_template.Get(realm->isolate);
    } else if (metadata.local_name == "iframe") {
      node_template =
          realm->html_iframe_element_template.Get(realm->isolate);
    } else if (metadata.local_name == "head") {
      node_template = realm->html_head_element_template.Get(realm->isolate);
    } else if (metadata.local_name == "script") {
      node_template = realm->html_script_element_template.Get(realm->isolate);
    } else if (metadata.local_name == "audio" ||
               metadata.local_name == "video") {
      node_template = realm->html_media_element_template.Get(realm->isolate);
    } else if (metadata.local_name == "img") {
      node_template = realm->html_image_element_template.Get(realm->isolate);
    } else {
      node_template = realm->html_element_template.Get(realm->isolate);
    }
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
  auto animation = realm->callback_animation_frames.find(callback);
  if (animation != realm->callback_animation_frames.end()) {
    realm->animation_frame_callbacks.erase(animation->second);
    realm->callback_animation_frames.erase(animation);
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

void HTMLTemplateElementContentGetter(
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
  uint64_t document = 0;
  uint32_t node = 0;
  char *host_error = nullptr;
  if (realm->active_host->template_content(
          realm->active_host->execution_id, key.document, key.node, &document,
          &node, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "reading template.content failed"
                                      : error);
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

void TextSplitText(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  int32_t offset = 0;
  if (!ReadReceiverKey(isolate, info.This(), &key) || info.Length() == 0 ||
      !info[0]->Int32Value(isolate->GetCurrentContext()).To(&offset))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  uint64_t document = 0;
  uint32_t node = 0;
  char *host_error = nullptr;
  if (realm->active_host->split_text(realm->active_host->execution_id,
                                     key.document, key.node, offset,
                                     &document, &node, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "splitText failed" : error);
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

void NodeNormalize(const v8::FunctionCallbackInfo<v8::Value> &info) {
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
  char *host_error = nullptr;
  if (realm->active_host->normalize_node(
          realm->active_host->execution_id, key.document, key.node,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "normalize failed" : error);
    return;
  }
  std::free(host_error);
}

void DocumentImportOrAdoptNode(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (info.Length() == 0 || !info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &key)) {
    ThrowTypeError(isolate, "importNode/adoptNode requires a native Node");
    return;
  }
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  uint64_t document = 0;
  uint32_t node = 0;
  char *host_error = nullptr;
  bool adopt = info.Data()->IsTrue();
  int ok = adopt
               ? realm->active_host->adopt_node(
                     realm->active_host->execution_id, key.document, key.node,
                     &document, &node, &host_error)
               : realm->active_host->clone_node(
                     realm->active_host->execution_id, key.document, key.node,
                     info.Length() > 1 && info[1]->BooleanValue(isolate) ? 1
                                                                         : 0,
                     &document, &node, &host_error);
  if (ok == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? (adopt ? "adoptNode failed"
                                               : "importNode failed")
                                      : error);
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

void DocumentReadyStateGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *state = nullptr;
  size_t state_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->document_ready_state(
          realm->active_host->execution_id, &state, &state_length,
          &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(state);
    ThrowError(isolate, error.empty() ? "reading document.readyState failed"
                                      : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> value;
  bool allocated = NewUTF8String(isolate, state == nullptr ? "" : state,
                                 state_length, &value);
  std::free(state);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate document.readyState");
    return;
  }
  info.GetReturnValue().Set(value);
}

void DocumentTitleGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *title = nullptr;
  size_t title_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->document_title(
          realm->active_host->execution_id, &title, &title_length,
          &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(title);
    ThrowError(isolate, error.empty() ? "reading document.title failed"
                                      : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> value;
  bool allocated = NewUTF8String(isolate, title == nullptr ? "" : title,
                                 title_length, &value);
  std::free(title);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate document.title");
    return;
  }
  info.GetReturnValue().Set(value);
}

void DocumentTitleSetter(
    v8::Local<v8::Name>, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<void> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::String> rendered;
  if (!value->ToString(isolate->GetCurrentContext()).ToLocal(&rendered))
    return;
  std::string title = UTF8Value(isolate, rendered);
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->set_document_title(
          realm->active_host->execution_id, title.data(), title.size(),
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "updating document.title failed"
                                      : error);
    return;
  }
  std::free(host_error);
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
  else if (name == "documentElement" || name == "scrollingElement")
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

bool ReadFormControlNodes(gossamer_v8_realm *realm, const WrapperKey &key,
                          FacadeKind kind, std::vector<uint32_t> *nodes,
                          std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  uint8_t host_kind = kind == FacadeKind::SelectOptions ? 1 : 2;
  uint32_t *host_nodes = nullptr;
  size_t count = 0;
  char *host_error = nullptr;
  if (realm->active_host->form_control_nodes(
          realm->active_host->execution_id, key.document, key.node, host_kind,
          &host_nodes, &count, &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(host_nodes);
    if (error->empty())
      *error = "reading form controls failed";
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

bool ReadCollectionNodes(gossamer_v8_realm *realm, const WrapperKey &key,
                         FacadeKind kind, std::vector<uint32_t> *nodes,
                         std::string *error) {
  if (kind == FacadeKind::SelectOptions || kind == FacadeKind::FormElements)
    return ReadFormControlNodes(realm, key, kind, nodes, error);
  return ReadChildNodes(realm, key, kind == FacadeKind::HTMLCollection, nodes,
                        error);
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

FacadeKind FacadeKindForObject(v8::Local<v8::Object> facade,
                               FacadeKind fallback) {
  if (facade.IsEmpty() ||
      facade->InternalFieldCount() < kFacadeInternalFieldCount)
    return fallback;
  v8::Local<v8::Data> data = facade->GetInternalField(kFacadeBackingField);
  if (!data->IsValue() || !data.As<v8::Value>()->IsInt32())
    return fallback;
  return static_cast<FacadeKind>(
      data.As<v8::Value>().As<v8::Int32>()->Value());
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
  if (!ReadCollectionNodes(realm, key, kind, &nodes, error))
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
  if (!ReadCollectionNodes(realm, key, kind, &nodes, error))
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
  FacadeKind kind = FacadeKindForObject(
      info.Holder(), FacadeKindFromData(info.Data()));
  if (!ReadFacadeLength(CurrentRealm(isolate), isolate, info.Holder(), kind,
                        &length, &error)) {
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
  FacadeKind kind = FacadeKindForObject(
      info.Holder(), FacadeKindFromData(info.Data()));
  if (!ReadFacadeValue(CurrentRealm(isolate), isolate->GetCurrentContext(),
                       info.Holder(), kind, index, &found, &error)
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
  FacadeKind kind = FacadeKindForObject(
      info.Holder(), FacadeKindFromData(info.Data()));
  if (!ReadFacadeLength(CurrentRealm(isolate), isolate, info.Holder(), kind,
                        &length, &error)) {
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
  FacadeKind kind = FacadeKindForObject(
      info.Holder(), FacadeKindFromData(info.Data()));
  if (!ReadFacadeLength(CurrentRealm(isolate), isolate, info.Holder(), kind,
                        &length, &error)) {
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
  FacadeKind kind = FacadeKindForObject(
      info.This(), FacadeKindFromData(info.Data()));
  if (!ReadFacadeValue(CurrentRealm(isolate), isolate->GetCurrentContext(),
                       info.This(), kind, index, &found, &error)
           .ToLocal(&value)) {
    ThrowError(isolate, error.empty() ? "reading DOM facade item failed"
                                      : error);
    return;
  }
  info.GetReturnValue().Set(found ? value : v8::Null(isolate));
}

bool FindNamedCollectionItem(gossamer_v8_realm *realm, const WrapperKey &key,
                             FacadeKind kind, const std::string &name,
                             uint32_t *node, bool *found,
                             std::string *error) {
  std::vector<uint32_t> nodes;
  if (!ReadCollectionNodes(realm, key, kind, &nodes, error))
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
  FacadeKind kind = FacadeKindForObject(info.This(),
                                        FacadeKind::HTMLCollection);
  if (!FindNamedCollectionItem(realm, key, kind, name, &node, &found,
                               &error)) {
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
  FacadeKind kind = FacadeKindForObject(info.Holder(),
                                        FacadeKind::HTMLCollection);
  if (!FindNamedCollectionItem(realm, key, kind, name, &node, &found,
                               &error)) {
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
  FacadeKind kind = FacadeKindForObject(info.Holder(),
                                        FacadeKind::HTMLCollection);
  if (!FindNamedCollectionItem(CurrentRealm(isolate), key, kind,
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
  FacadeKind kind = FacadeKindForObject(info.Holder(),
                                        FacadeKind::HTMLCollection);
  if (!ReadCollectionNodes(realm, key, kind, &nodes, &error)) {
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
  FacadeKind kind = FacadeKindForObject(
      info.This(), static_cast<FacadeKind>(encoded / 10));
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
  FacadeKind kind = FacadeKindForObject(
      info.This(), FacadeKindFromData(info.Data()));
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

void DocumentGetElementsByTagName(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey scope;
  if (!ReadReceiverKey(isolate, info.This(), &scope))
    return;
  std::string tag;
  if (!StringFromValue(isolate,
                       info.Length() == 0 ? v8::Undefined(isolate) : info[0],
                       &tag))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::vector<uint32_t> nodes;
  std::string error;
  if (!ReadSelectorNodes(realm, scope, tag, true, &nodes, &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::Object> list;
  if (!CreateStaticNodeList(realm, isolate->GetCurrentContext(), scope, nodes,
                            &error)
           .ToLocal(&list)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(list);
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

void ElementFormIndeterminateGetter(
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
  int indeterminate = 0;
  char *host_error = nullptr;
  if (realm->active_host->form_indeterminate(
          realm->active_host->execution_id, key.document, key.node,
          &indeterminate, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty()
                            ? "reading form indeterminate state failed"
                            : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(indeterminate != 0);
}

void ElementFormIndeterminateSetter(
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
  if (realm->active_host->set_form_indeterminate(
          realm->active_host->execution_id, key.document, key.node,
          value->BooleanValue(isolate) ? 1 : 0, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty()
                            ? "setting form indeterminate state failed"
                            : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

void ElementFormSelectedGetter(
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
  int selected = 0;
  char *host_error = nullptr;
  if (realm->active_host->form_selected(
          realm->active_host->execution_id, key.document, key.node, &selected,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "reading form selected state failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(selected != 0);
}

void ElementFormSelectedSetter(
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
  if (realm->active_host->set_form_selected(
          realm->active_host->execution_id, key.document, key.node,
          value->BooleanValue(isolate) ? 1 : 0, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "setting form selected state failed" : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

void ElementFormSelectedIndexGetter(
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
  int32_t index = -1;
  char *host_error = nullptr;
  if (realm->active_host->form_selected_index(
          realm->active_host->execution_id, key.document, key.node, &index,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "reading selectedIndex failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(v8::Integer::New(isolate, index));
}

void ElementFormSelectedIndexSetter(
    v8::Local<v8::Name>, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  int32_t index = -1;
  if (!ReadReceiverKey(isolate, info.Holder(), &key) ||
      !value->Int32Value(isolate->GetCurrentContext()).To(&index)) {
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
  if (realm->active_host->set_form_selected_index(
          realm->active_host->execution_id, key.document, key.node, index,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "setting selectedIndex failed" : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

bool ReadHostFormSelection(gossamer_v8_realm *realm, const WrapperKey &key,
                           int32_t *start, int32_t *end,
                           std::string *direction, std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_direction = nullptr;
  size_t direction_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->form_selection(
          realm->active_host->execution_id, key.document, key.node, start,
          end, &host_direction, &direction_length, &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(host_direction);
    if (error->empty())
      *error = "reading text selection failed";
    return false;
  }
  std::free(host_error);
  direction->assign(host_direction == nullptr ? "" : host_direction,
                    direction_length);
  std::free(host_direction);
  return true;
}

bool WriteHostFormSelection(gossamer_v8_realm *realm, const WrapperKey &key,
                            int32_t start, int32_t end,
                            const std::string &direction,
                            std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->set_form_selection(
          realm->active_host->execution_id, key.document, key.node, start,
          end, direction.data(), direction.size(), &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "setting text selection failed";
    return false;
  }
  std::free(host_error);
  return true;
}

void ElementFormSelectionGetter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  int32_t start = 0;
  int32_t end = 0;
  std::string direction;
  std::string error;
  if (!ReadHostFormSelection(CurrentRealm(isolate), key, &start, &end,
                             &direction, &error)) {
    ThrowError(isolate, error);
    return;
  }
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  if (name == "selectionStart") {
    info.GetReturnValue().Set(v8::Integer::New(isolate, start));
  } else if (name == "selectionEnd") {
    info.GetReturnValue().Set(v8::Integer::New(isolate, end));
  } else {
    v8::Local<v8::String> rendered;
    if (!NewUTF8String(isolate, direction.data(), direction.size(),
                       &rendered)) {
      ThrowError(isolate, "V8 failed to allocate selectionDirection");
      return;
    }
    info.GetReturnValue().Set(rendered);
  }
}

void ElementFormSelectionSetter(
    v8::Local<v8::Name> property, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key)) {
    info.GetReturnValue().Set(false);
    return;
  }
  int32_t start = 0;
  int32_t end = 0;
  std::string direction;
  std::string error;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (!ReadHostFormSelection(realm, key, &start, &end, &direction, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  if (name == "selectionDirection") {
    if (!StringFromValue(isolate, value, &direction)) {
      info.GetReturnValue().Set(false);
      return;
    }
  } else {
    int32_t offset = 0;
    if (!value->Int32Value(isolate->GetCurrentContext()).To(&offset)) {
      info.GetReturnValue().Set(false);
      return;
    }
    if (name == "selectionStart") {
      start = offset;
      if (start > end)
        end = start;
    } else {
      end = offset;
      if (end < start)
        start = end;
    }
  }
  if (!WriteHostFormSelection(realm, key, start, end, direction, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  info.GetReturnValue().Set(true);
}

void ElementFormSelectionFunctionGetter(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  int32_t start = 0;
  int32_t end = 0;
  std::string direction;
  std::string error;
  if (!ReadHostFormSelection(CurrentRealm(isolate), key, &start, &end,
                             &direction, &error)) {
    ThrowError(isolate, error);
    return;
  }
  int property = info.Data().As<v8::Int32>()->Value();
  if (property == 1) {
    info.GetReturnValue().Set(v8::Integer::New(isolate, start));
  } else if (property == 2) {
    info.GetReturnValue().Set(v8::Integer::New(isolate, end));
  } else {
    v8::Local<v8::String> rendered;
    if (!NewUTF8String(isolate, direction.data(), direction.size(),
                       &rendered)) {
      ThrowError(isolate, "V8 failed to allocate selectionDirection");
      return;
    }
    info.GetReturnValue().Set(rendered);
  }
}

void ElementFormSelectionFunctionSetter(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key) || info.Length() == 0)
    return;
  int32_t start = 0;
  int32_t end = 0;
  std::string direction;
  std::string error;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (!ReadHostFormSelection(realm, key, &start, &end, &direction, &error)) {
    ThrowError(isolate, error);
    return;
  }
  int property = info.Data().As<v8::Int32>()->Value();
  if (property == 3) {
    if (!StringFromValue(isolate, info[0], &direction))
      return;
  } else {
    int32_t offset = 0;
    if (!info[0]->Int32Value(isolate->GetCurrentContext()).To(&offset))
      return;
    if (property == 1) {
      start = offset;
      if (start > end)
        end = start;
    } else {
      end = offset;
      if (end < start)
        start = end;
    }
  }
  if (!WriteHostFormSelection(realm, key, start, end, direction, &error))
    ThrowError(isolate, error);
}

void ElementSetSelectionRange(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  int32_t start = 0;
  int32_t end = 0;
  if (!ReadReceiverKey(isolate, info.This(), &key) || info.Length() < 2 ||
      !info[0]->Int32Value(isolate->GetCurrentContext()).To(&start) ||
      !info[1]->Int32Value(isolate->GetCurrentContext()).To(&end))
    return;
  std::string direction = "none";
  if (info.Length() > 2 && !StringFromValue(isolate, info[2], &direction))
    return;
  std::string error;
  if (!WriteHostFormSelection(CurrentRealm(isolate), key, start, end,
                              direction, &error))
    ThrowError(isolate, error);
}

void ElementSelectText(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  std::string error;
  if (!WriteHostFormSelection(CurrentRealm(isolate), key, 0,
                              std::numeric_limits<int32_t>::max(), "none",
                              &error))
    ThrowError(isolate, error);
}

void ElementFormOwnerGetter(
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
  uint32_t owner = 0;
  int found = 0;
  char *host_error = nullptr;
  if (realm->active_host->form_owner(
          realm->active_host->execution_id, key.document, key.node, &owner,
          &found, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "reading form owner failed" : error);
    return;
  }
  std::free(host_error);
  if (found == 0) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(realm, isolate->GetCurrentContext(),
                              WrapperKey{key.document, owner}, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error.empty() ? "wrapping form owner failed" : error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void ElementFormCollectionGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Object> node = info.Holder();
  WrapperKey key;
  if (!ReadReceiverKey(isolate, node, &key))
    return;
  v8::Local<v8::Data> cached_data =
      node->GetInternalField(kNodeFormCollectionField);
  if (cached_data->IsValue()) {
    v8::Local<v8::Value> cached = cached_data.As<v8::Value>();
    if (cached->IsObject()) {
      info.GetReturnValue().Set(cached);
      return;
    }
  }
  FacadeKind kind = FacadeKindFromData(info.Data());
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  v8::Local<v8::Object> facade;
  if (!realm->html_collection_template.Get(isolate)
           ->InstanceTemplate()
           ->NewInstance(isolate->GetCurrentContext())
           .ToLocal(&facade)) {
    ThrowError(isolate, "V8 failed to allocate a live form collection");
    return;
  }
  facade->SetInternalField(kFacadeNodeField, node);
  facade->SetInternalField(kFacadeBackingField,
                           v8::Integer::New(isolate, static_cast<int>(kind)));
  node->SetInternalField(kNodeFormCollectionField, facade);
  info.GetReturnValue().Set(facade);
}

void HTMLFormElementReset(
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
  char *host_error = nullptr;
  if (realm->active_host->reset_form(realm->active_host->execution_id,
                                     key.document, key.node,
                                     &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "resetting form failed" : error);
    return;
  }
  std::free(host_error);
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

void ElementFormIndeterminateFunctionGetter(
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
  int indeterminate = 0;
  char *host_error = nullptr;
  if (realm->active_host->form_indeterminate(
          realm->active_host->execution_id, key.document, key.node,
          &indeterminate, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty()
                            ? "reading form indeterminate state failed"
                            : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(indeterminate != 0);
}

void ElementFormIndeterminateFunctionSetter(
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
  if (realm->active_host->set_form_indeterminate(
          realm->active_host->execution_id, key.document, key.node,
          info[0]->BooleanValue(isolate) ? 1 : 0, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty()
                            ? "setting form indeterminate state failed"
                            : error);
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
  if (property == "defaultSelected")
    return "selected";
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

void NodeConvenienceMutation(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey receiver;
  if (!ReadReceiverKey(isolate, info.This(), &receiver))
    return;
  DOMMutationOperation operation = static_cast<DOMMutationOperation>(
      info.Data().As<v8::Uint32>()->Value());
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }

  std::vector<uint64_t> documents;
  std::vector<uint32_t> nodes;
  if (operation != DOMMutationOperation::Remove) {
    documents.reserve(info.Length());
    nodes.reserve(info.Length());
    for (int index = 0; index < info.Length(); ++index) {
      WrapperKey argument;
      if (info[index]->IsObject() &&
          ReadWrapperKey(info[index].As<v8::Object>(), &argument)) {
        documents.push_back(argument.document);
        nodes.push_back(argument.node);
        continue;
      }
      std::string text;
      if (!StringFromValue(isolate, info[index], &text))
        return;
      char *host_error = nullptr;
      if (realm->active_host->create_text_node(
              realm->active_host->execution_id, text.data(), text.size(),
              &argument.document, &argument.node, &host_error) == 0) {
        error = TakeCString(host_error);
        ThrowError(isolate,
                   error.empty() ? "creating mutation text failed" : error);
        return;
      }
      std::free(host_error);
      documents.push_back(argument.document);
      nodes.push_back(argument.node);
    }
  }

  char *host_error = nullptr;
  if (realm->active_host->mutate_nodes(
          realm->active_host->execution_id, receiver.document, receiver.node,
          static_cast<uint8_t>(operation),
          documents.empty() ? nullptr : documents.data(),
          nodes.empty() ? nullptr : nodes.data(), nodes.size(),
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "DOM mutation failed" : error);
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
  v8::Local<v8::FunctionTemplate> style_template =
      realm->style_template.Get(isolate);
  v8::Local<v8::Object> style;
  if (!style_template->InstanceTemplate()
           ->NewInstance(isolate->GetCurrentContext())
           .ToLocal(&style)) {
    ThrowError(isolate, "V8 failed to allocate element.style");
    return;
  }
  style->SetInternalField(kStyleNodeField, node);
  style->SetInternalField(kStyleComputedField, v8::False(isolate));
  style->SetInternalField(kStylePseudoField, v8::String::Empty(isolate));
  node->SetInternalField(kNodeStyleField, style);
  info.GetReturnValue().Set(style);
}

bool ReadComputedStyleProperty(gossamer_v8_realm *realm,
                               const StyleReference &reference,
                               const std::string &name, std::string *value,
                               bool *found, std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_value = nullptr;
  size_t value_length = 0;
  int host_found = 0;
  char *host_error = nullptr;
  if (realm->active_host->computed_style_property(
          realm->active_host->execution_id, reference.key.document,
          reference.key.node, reference.pseudo.data(), reference.pseudo.size(),
          name.data(), name.size(), &host_value, &value_length, &host_found,
          &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(host_value);
    if (error->empty())
      *error = "reading computed style property failed";
    return false;
  }
  std::free(host_error);
  value->assign(host_value == nullptr ? "" : host_value, value_length);
  *found = host_found != 0;
  std::free(host_value);
  return true;
}

bool ReadComputedStyleCount(gossamer_v8_realm *realm,
                            const StyleReference &reference, size_t *count,
                            std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->computed_style_property_count(
          realm->active_host->execution_id, reference.key.document,
          reference.key.node, reference.pseudo.data(), reference.pseudo.size(),
          count, &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "reading computed style length failed";
    return false;
  }
  std::free(host_error);
  return true;
}

bool ReadComputedStyleName(gossamer_v8_realm *realm,
                           const StyleReference &reference, size_t index,
                           std::string *name, bool *found,
                           std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_name = nullptr;
  size_t name_length = 0;
  int host_found = 0;
  char *host_error = nullptr;
  if (realm->active_host->computed_style_property_name(
          realm->active_host->execution_id, reference.key.document,
          reference.key.node, reference.pseudo.data(), reference.pseudo.size(),
          index, &host_name, &name_length, &host_found, &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(host_name);
    if (error->empty())
      *error = "reading computed style item failed";
    return false;
  }
  std::free(host_error);
  name->assign(host_name == nullptr ? "" : host_name, name_length);
  *found = host_found != 0;
  std::free(host_name);
  return true;
}

bool ReadStylePropertyCount(gossamer_v8_realm *realm,
                            const StyleReference &reference, size_t *count,
                            std::string *error) {
  if (reference.computed)
    return ReadComputedStyleCount(realm, reference, count, error);
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->style_property_count(
          realm->active_host->execution_id, reference.key.document,
          reference.key.node, count, &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "reading style length failed";
    return false;
  }
  std::free(host_error);
  return true;
}

bool ReadStylePropertyName(gossamer_v8_realm *realm,
                           const StyleReference &reference, size_t index,
                           std::string *name, bool *found,
                           std::string *error) {
  if (reference.computed)
    return ReadComputedStyleName(realm, reference, index, name, found, error);
  if (!RequireHost(realm, error))
    return false;
  char *host_name = nullptr;
  size_t name_length = 0;
  int host_found = 0;
  char *host_error = nullptr;
  if (realm->active_host->style_property_name(
          realm->active_host->execution_id, reference.key.document,
          reference.key.node, index, &host_name, &name_length, &host_found,
          &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(host_name);
    if (error->empty())
      *error = "reading style item failed";
    return false;
  }
  std::free(host_error);
  name->assign(host_name == nullptr ? "" : host_name, name_length);
  *found = host_found != 0;
  std::free(host_name);
  return true;
}

bool RequireMutableStyle(v8::Isolate *isolate,
                         const StyleReference &reference) {
  if (!reference.computed)
    return true;
  ThrowTypeError(isolate, "Computed style declarations are read-only");
  return false;
}

void StyleCSSTextGetter(v8::Local<v8::Name>,
                        const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference))
    return;
  std::string error;
  if (reference.computed) {
    // CSSOM exposes an empty cssText for computed declarations. Still consult
    // the host so a retained declaration observes stale handles and document
    // lifecycle failures rather than becoming a detached snapshot.
    size_t ignored = 0;
    if (!ReadComputedStyleCount(realm, reference, &ignored, &error)) {
      ThrowError(isolate, error);
      return;
    }
    info.GetReturnValue().Set(v8::String::Empty(isolate));
    return;
  }
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *value = nullptr;
  size_t value_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->style_css_text(
          realm->active_host->execution_id, reference.key.document,
          reference.key.node, &value, &value_length, &host_error) == 0) {
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
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference) ||
      !RequireMutableStyle(isolate, reference)) {
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
          realm->active_host->execution_id, reference.key.document,
          reference.key.node, rendered.data(), rendered.size(),
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "setting cssText failed" : error);
    info.GetReturnValue().Set(false);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(true);
}

bool ReadStyleProperty(gossamer_v8_realm *realm,
                       const StyleReference &reference,
                       const std::string &name, std::string *value,
                       std::string *priority, bool *found,
                       std::string *error) {
  if (reference.computed) {
    priority->clear();
    return ReadComputedStyleProperty(realm, reference, name, value, found,
                                     error);
  }
  if (!RequireHost(realm, error))
    return false;
  char *host_value = nullptr;
  size_t value_length = 0;
  char *host_priority = nullptr;
  size_t priority_length = 0;
  int host_found = 0;
  char *host_error = nullptr;
  if (realm->active_host->style_property(
          realm->active_host->execution_id, reference.key.document,
          reference.key.node, name.data(), name.size(), &host_value,
          &value_length, &host_priority, &priority_length, &host_found,
          &host_error) == 0) {
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
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference))
    return;
  std::string error;
  size_t count = 0;
  if (!ReadStylePropertyCount(realm, reference, &count, &error)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(
      v8::Number::New(isolate, static_cast<double>(count)));
}

void StyleItem(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.This(), &reference))
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
  std::string name;
  bool found = false;
  if (!ReadStylePropertyName(realm, reference, static_cast<size_t>(index),
                             &name, &found, &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::String> result;
  if (!NewUTF8String(isolate, name.data(), found ? name.size() : 0, &result)) {
    ThrowError(isolate, "V8 failed to allocate style item");
    return;
  }
  info.GetReturnValue().Set(result);
}

void StyleGetPropertyValue(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.This(), &reference))
    return;
  std::string name;
  if (info.Length() == 0 || !StringFromValue(isolate, info[0], &name))
    return;
  std::string value;
  std::string priority;
  std::string error;
  bool found = false;
  if (!ReadStyleProperty(realm, reference, name, &value, &priority, &found,
                         &error)) {
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
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.This(), &reference))
    return;
  std::string name;
  if (info.Length() == 0 || !StringFromValue(isolate, info[0], &name))
    return;
  std::string value;
  std::string priority;
  std::string error;
  bool found = false;
  if (!ReadStyleProperty(realm, reference, name, &value, &priority, &found,
                         &error)) {
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
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.This(), &reference) ||
      !RequireMutableStyle(isolate, reference))
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
  if (!SetStyleProperty(realm, reference.key, name, value, priority, &error))
    ThrowError(isolate, error);
}

void StyleRemoveProperty(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.This(), &reference) ||
      !RequireMutableStyle(isolate, reference))
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
          realm->active_host->execution_id, reference.key.document,
          reference.key.node, name.data(), name.size(), &value, &value_length,
          &host_error) == 0) {
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
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference))
    return;
  std::string name =
      CSSPropertyNameFromJS(UTF8Value(isolate, property.As<v8::Value>()));
  std::string value;
  std::string priority;
  std::string error;
  bool found = false;
  if (!ReadStyleProperty(realm, reference, name, &value, &priority, &found,
                         &error)) {
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
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference) ||
      !RequireMutableStyle(isolate, reference)) {
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
  if (!SetStyleProperty(realm, reference.key, name, rendered, "", &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  info.GetReturnValue().Set(true);
}

v8::Intercepted StyleIndexedGetter(
    uint32_t index, const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference))
    return v8::Intercepted::kYes;
  std::string name;
  bool found = false;
  std::string error;
  if (!ReadStylePropertyName(CurrentRealm(isolate), reference, index, &name,
                             &found, &error)) {
    ThrowError(isolate, error);
    return v8::Intercepted::kYes;
  }
  if (!found)
    return v8::Intercepted::kNo;
  v8::Local<v8::String> result;
  if (!NewUTF8String(isolate, name.data(), name.size(), &result)) {
    ThrowError(isolate, "V8 failed to allocate a style index");
    return v8::Intercepted::kYes;
  }
  info.GetReturnValue().Set(result);
  return v8::Intercepted::kYes;
}

v8::Intercepted StyleIndexedSetter(
    uint32_t index, v8::Local<v8::Value>,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference)) {
    info.GetReturnValue().Set(false);
    return v8::Intercepted::kYes;
  }
  size_t count = 0;
  std::string error;
  if (!ReadStylePropertyCount(CurrentRealm(isolate), reference, &count,
                              &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return v8::Intercepted::kYes;
  }
  if (index >= count)
    return v8::Intercepted::kNo;
  if (reference.computed)
    RequireMutableStyle(isolate, reference);
  info.GetReturnValue().Set(false);
  return v8::Intercepted::kYes;
}

v8::Intercepted StyleIndexedQuery(
    uint32_t index, const v8::PropertyCallbackInfo<v8::Integer> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference))
    return v8::Intercepted::kYes;
  size_t count = 0;
  std::string error;
  if (!ReadStylePropertyCount(CurrentRealm(isolate), reference, &count,
                              &error)) {
    ThrowError(isolate, error);
    return v8::Intercepted::kYes;
  }
  if (index >= count)
    return v8::Intercepted::kNo;
  info.GetReturnValue().Set(v8::None);
  return v8::Intercepted::kYes;
}

void StyleIndexedEnumerator(
    const v8::PropertyCallbackInfo<v8::Array> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference))
    return;
  size_t count = 0;
  std::string error;
  if (!ReadStylePropertyCount(CurrentRealm(isolate), reference, &count,
                              &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (count > static_cast<size_t>(std::numeric_limits<int>::max())) {
    ThrowError(isolate, "style declaration exceeds V8's enumeration limit");
    return;
  }
  v8::Local<v8::Array> indices =
      v8::Array::New(isolate, static_cast<int>(count));
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  for (size_t index = 0; index < count; ++index) {
    if (!indices
             ->Set(context, static_cast<uint32_t>(index),
                   v8::Integer::NewFromUnsigned(isolate,
                                                static_cast<uint32_t>(index)))
             .FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to enumerate style indices");
      return;
    }
  }
  info.GetReturnValue().Set(indices);
}

v8::Intercepted StyleNamedGetter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Value> &info) {
  if (!property->IsString())
    return v8::Intercepted::kNo;
  v8::Isolate *isolate = info.GetIsolate();
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  if (!IsDashedStylePropertyName(name))
    return v8::Intercepted::kNo;
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference))
    return v8::Intercepted::kYes;
  std::string value;
  std::string priority;
  bool found = false;
  std::string error;
  if (!ReadStyleProperty(CurrentRealm(isolate), reference, name, &value,
                         &priority, &found, &error)) {
    ThrowError(isolate, error);
    return v8::Intercepted::kYes;
  }
  if (!found && !IsSupportedDashedStylePropertyName(name))
    return v8::Intercepted::kNo;
  v8::Local<v8::String> result;
  if (!NewUTF8String(isolate, value.data(), value.size(), &result)) {
    ThrowError(isolate, "V8 failed to allocate a named style property");
    return v8::Intercepted::kYes;
  }
  info.GetReturnValue().Set(result);
  return v8::Intercepted::kYes;
}

v8::Intercepted StyleNamedSetter(
    v8::Local<v8::Name> property, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  if (!property->IsString())
    return v8::Intercepted::kNo;
  v8::Isolate *isolate = info.GetIsolate();
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  if (!IsDashedStylePropertyName(name))
    return v8::Intercepted::kNo;
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference) ||
      !RequireMutableStyle(isolate, reference)) {
    info.GetReturnValue().Set(false);
    return v8::Intercepted::kYes;
  }
  std::string rendered;
  if (!StringFromValue(isolate, value, &rendered)) {
    info.GetReturnValue().Set(false);
    return v8::Intercepted::kYes;
  }
  std::string error;
  if (!SetStyleProperty(CurrentRealm(isolate), reference.key, name, rendered,
                        "", &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return v8::Intercepted::kYes;
  }
  info.GetReturnValue().Set(true);
  return v8::Intercepted::kYes;
}

v8::Intercepted StyleNamedQuery(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Integer> &info) {
  if (!property->IsString())
    return v8::Intercepted::kNo;
  v8::Isolate *isolate = info.GetIsolate();
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  if (!IsDashedStylePropertyName(name))
    return v8::Intercepted::kNo;
  StyleReference reference;
  if (!ReadStyleReference(isolate, info.Holder(), &reference))
    return v8::Intercepted::kYes;
  std::string value;
  std::string priority;
  bool found = false;
  std::string error;
  if (!ReadStyleProperty(CurrentRealm(isolate), reference, name, &value,
                         &priority, &found, &error)) {
    ThrowError(isolate, error);
    return v8::Intercepted::kYes;
  }
  if (!found && !IsSupportedDashedStylePropertyName(name))
    return v8::Intercepted::kNo;
  info.GetReturnValue().Set(v8::None);
  return v8::Intercepted::kYes;
}

void GetComputedStyle(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (info.Length() == 0 || !info[0]->IsObject()) {
    ThrowTypeError(isolate, "getComputedStyle requires an Element");
    return;
  }
  v8::Local<v8::Object> node = info[0].As<v8::Object>();
  WrapperKey key;
  if (!ReadWrapperKey(node, &key)) {
    ThrowTypeError(isolate, "getComputedStyle requires an Element");
    return;
  }
  NodeMetadata metadata;
  std::string error;
  if (!ReadNodeMetadata(realm, key, &metadata, &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (metadata.type != 1) {
    ThrowTypeError(isolate, "getComputedStyle requires an Element");
    return;
  }
  std::string pseudo;
  if (info.Length() > 1 && !info[1]->IsUndefined() && !info[1]->IsNull() &&
      !StringFromValue(isolate, info[1], &pseudo)) {
    return;
  }
  std::string pseudo_error;
  if (!ValidateComputedStylePseudo(pseudo, &pseudo_error)) {
    ThrowTypeError(isolate, pseudo_error);
    return;
  }
  v8::Local<v8::String> pseudo_value;
  if (!NewUTF8String(isolate, pseudo.data(), pseudo.size(), &pseudo_value)) {
    ThrowError(isolate, "V8 failed to allocate computed style pseudo value");
    return;
  }
  v8::Local<v8::FunctionTemplate> style_template =
      realm->style_template.Get(isolate);
  v8::Local<v8::Object> declaration;
  if (!style_template->InstanceTemplate()
           ->NewInstance(isolate->GetCurrentContext())
           .ToLocal(&declaration)) {
    ThrowError(isolate, "V8 failed to allocate computed style declaration");
    return;
  }
  declaration->SetInternalField(kStyleNodeField, node);
  declaration->SetInternalField(kStyleComputedField, v8::True(isolate));
  declaration->SetInternalField(kStylePseudoField, pseudo_value);
  info.GetReturnValue().Set(declaration);
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
  if (state->interface == EventInterface::CustomEvent) {
    v8::Local<v8::Context> context = isolate->GetCurrentContext();
    v8::Local<v8::Value> detail = v8::Null(isolate);
    if (info.Length() > 1 && info[1]->IsObject()) {
      v8::Local<v8::Object> options = info[1].As<v8::Object>();
      if (!options
               ->Get(context,
                     v8::String::NewFromUtf8Literal(isolate, "detail"))
               .ToLocal(&detail))
        return;
    }
    if (!info.This()
             ->DefineOwnProperty(
                 context,
                 v8::String::NewFromUtf8Literal(isolate, "detail"), detail,
                 static_cast<v8::PropertyAttribute>(
                     v8::ReadOnly | v8::DontEnum | v8::DontDelete))
             .FromMaybe(false))
      return;
  }
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  TrackEventObject(realm, info.This(), state.release());
  info.GetReturnValue().Set(info.This());
}

MutationObserverState *ReadMutationObserverState(
    v8::Isolate *isolate, v8::Local<v8::Object> receiver) {
  if (receiver.IsEmpty() || receiver->InternalFieldCount() != 1) {
    ThrowTypeError(isolate, "MutationObserver receiver is invalid");
    return nullptr;
  }
  v8::Local<v8::Data> data =
      receiver->GetInternalField(kMutationObserverStateField);
  if (!data->IsValue() || !data.As<v8::Value>()->IsExternal()) {
    ThrowTypeError(isolate, "MutationObserver lost its native state");
    return nullptr;
  }
  return static_cast<MutationObserverState *>(
      data.As<v8::Value>().As<v8::External>()->Value(
          v8::kExternalPointerTypeTagDefault));
}

void MutationObserverCollected(
    const v8::WeakCallbackInfo<MutationObserverWeakData> &info) {
  MutationObserverWeakData *weak = info.GetParameter();
  if (weak == nullptr)
    return;
  if (weak->realm != nullptr)
    weak->realm->mutation_observers.erase(weak);
  weak->object.Reset();
  if (weak->state != nullptr) {
    weak->state->callback.Reset();
    weak->state->registrations.clear();
    delete weak->state;
  }
  delete weak;
}

bool CurrentMutationSequence(gossamer_v8_realm *realm, uint64_t *sequence,
                             std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->mutation_sequence(
          realm->active_host->execution_id, sequence, &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "reading mutation sequence failed";
    return false;
  }
  std::free(host_error);
  return true;
}

bool MutationTargetMatches(gossamer_v8_realm *realm,
                           const MutationObserverRegistration &registration,
                           const WrapperKey &target, std::string *error) {
  if (registration.target == target)
    return true;
  if (!registration.options.subtree ||
      registration.target.document != target.document)
    return false;
  int contains = 0;
  char *host_error = nullptr;
  if (realm->active_host->contains(
          realm->active_host->execution_id, registration.target.document,
          registration.target.node, target.document, target.node, &contains,
          &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "matching mutation observer subtree failed";
    return false;
  }
  std::free(host_error);
  return contains != 0;
}

void FreeHostMutationRecords(gossamer_v8_mutation_record *records,
                             size_t count) {
  if (records == nullptr)
    return;
  for (size_t index = 0; index < count; ++index) {
    std::free(records[index].added_nodes);
    std::free(records[index].removed_nodes);
    std::free(records[index].attribute_name);
    std::free(records[index].old_value);
  }
  std::free(records);
}

bool FetchMutationObserverRecords(MutationObserverState *state,
                                  std::string *error) {
  if (state == nullptr || state->realm == nullptr ||
      state->registrations.empty())
    return true;
  gossamer_v8_realm *realm = state->realm;
  if (!RequireHost(realm, error))
    return false;
  gossamer_v8_mutation_record *host_records = nullptr;
  size_t count = 0;
  uint64_t latest = state->cursor;
  char *host_error = nullptr;
  if (realm->active_host->mutation_records(
          realm->active_host->execution_id, state->cursor, &host_records,
          &count, &latest, &host_error) == 0) {
    *error = TakeCString(host_error);
    FreeHostMutationRecords(host_records, count);
    if (error->empty())
      *error = "reading native mutation records failed";
    return false;
  }
  std::free(host_error);
  state->cursor = latest;
  for (size_t index = 0; index < count; ++index) {
    const gossamer_v8_mutation_record &source = host_records[index];
    WrapperKey target{state->registrations.front().target.document,
                      source.target};
    const MutationObserverRegistration *matched = nullptr;
    for (const auto &registration : state->registrations) {
      bool type_matches =
          (source.type == 1 && registration.options.child_list) ||
          (source.type == 2 && registration.options.attributes) ||
          (source.type == 3 && registration.options.character_data);
      if (!type_matches)
        continue;
      if (source.type == 2 &&
          !registration.options.attribute_filter.empty()) {
        std::string attribute(source.attribute_name == nullptr
                                  ? ""
                                  : source.attribute_name,
                              source.attribute_name_length);
        if (registration.options.attribute_filter.find(attribute) ==
            registration.options.attribute_filter.end())
          continue;
      }
      if (!MutationTargetMatches(realm, registration, target, error)) {
        if (!error->empty()) {
          FreeHostMutationRecords(host_records, count);
          return false;
        }
        continue;
      }
      matched = &registration;
      break;
    }
    if (matched == nullptr)
      continue;
    ObserverMutationRecord record;
    record.sequence = source.sequence;
    record.type = source.type;
    record.target = target;
    if (source.added_count != 0)
      record.added_nodes.assign(source.added_nodes,
                                source.added_nodes + source.added_count);
    if (source.removed_count != 0)
      record.removed_nodes.assign(source.removed_nodes,
                                  source.removed_nodes + source.removed_count);
    record.previous_sibling = source.previous_sibling;
    record.has_previous_sibling = source.has_previous_sibling != 0;
    record.next_sibling = source.next_sibling;
    record.has_next_sibling = source.has_next_sibling != 0;
    record.attribute_name.assign(source.attribute_name == nullptr
                                     ? ""
                                     : source.attribute_name,
                                 source.attribute_name_length);
    record.old_value.assign(source.old_value == nullptr ? "" : source.old_value,
                            source.old_value_length);
    record.expose_old_value =
        source.old_value_present != 0 &&
        ((source.type == 2 && matched->options.attribute_old_value) ||
         (source.type == 3 && matched->options.character_data_old_value));
    state->records.push_back(std::move(record));
  }
  FreeHostMutationRecords(host_records, count);
  return true;
}

bool SetMutationRecordProperty(v8::Local<v8::Context> context,
                               v8::Local<v8::Object> object,
                               const char *name,
                               v8::Local<v8::Value> value) {
  return object
      ->Set(context,
            v8::String::NewFromUtf8(v8::Isolate::GetCurrent(), name)
                .ToLocalChecked(),
            value)
      .FromMaybe(false);
}

v8::MaybeLocal<v8::Object> BuildMutationRecordObject(
    gossamer_v8_realm *realm, v8::Local<v8::Context> context,
    const ObserverMutationRecord &record, std::string *error) {
  v8::Isolate *isolate = realm->isolate;
  v8::Local<v8::Object> result;
  if (!realm->mutation_record_template.Get(isolate)
           ->InstanceTemplate()
           ->NewInstance(context)
           .ToLocal(&result)) {
    *error = "V8 failed to allocate a MutationRecord";
    return {};
  }
  const char *type = record.type == 1
                         ? "childList"
                         : (record.type == 2 ? "attributes"
                                             : "characterData");
  v8::Local<v8::Object> target;
  v8::Local<v8::Object> added;
  v8::Local<v8::Object> removed;
  if (!GetOrCreateNodeWrapper(realm, context, record.target, error)
           .ToLocal(&target) ||
      !CreateStaticNodeList(realm, context, record.target, record.added_nodes,
                            error)
           .ToLocal(&added) ||
      !CreateStaticNodeList(realm, context, record.target,
                            record.removed_nodes, error)
           .ToLocal(&removed))
    return {};
  v8::Local<v8::Value> previous = v8::Null(isolate);
  v8::Local<v8::Value> next = v8::Null(isolate);
  if (record.has_previous_sibling) {
    v8::Local<v8::Object> wrapper;
    if (!GetOrCreateNodeWrapper(
             realm, context,
             WrapperKey{record.target.document, record.previous_sibling},
             error)
             .ToLocal(&wrapper))
      return {};
    previous = wrapper;
  }
  if (record.has_next_sibling) {
    v8::Local<v8::Object> wrapper;
    if (!GetOrCreateNodeWrapper(
             realm, context,
             WrapperKey{record.target.document, record.next_sibling}, error)
             .ToLocal(&wrapper))
      return {};
    next = wrapper;
  }
  v8::Local<v8::Value> attribute_name = v8::Null(isolate);
  if (record.type == 2) {
    v8::Local<v8::String> rendered;
    if (!NewUTF8String(isolate, record.attribute_name.data(),
                       record.attribute_name.size(), &rendered)) {
      *error = "V8 failed to allocate a mutation attribute name";
      return {};
    }
    attribute_name = rendered;
  }
  v8::Local<v8::Value> old_value = v8::Null(isolate);
  if (record.expose_old_value) {
    v8::Local<v8::String> rendered;
    if (!NewUTF8String(isolate, record.old_value.data(),
                       record.old_value.size(), &rendered)) {
      *error = "V8 failed to allocate a mutation oldValue";
      return {};
    }
    old_value = rendered;
  }
  if (!SetMutationRecordProperty(
          context, result, "type",
          v8::String::NewFromUtf8(isolate, type).ToLocalChecked()) ||
      !SetMutationRecordProperty(context, result, "target", target) ||
      !SetMutationRecordProperty(context, result, "addedNodes", added) ||
      !SetMutationRecordProperty(context, result, "removedNodes", removed) ||
      !SetMutationRecordProperty(context, result, "previousSibling", previous) ||
      !SetMutationRecordProperty(context, result, "nextSibling", next) ||
      !SetMutationRecordProperty(context, result, "attributeName",
                                 attribute_name) ||
      !SetMutationRecordProperty(context, result, "attributeNamespace",
                                 v8::Null(isolate)) ||
      !SetMutationRecordProperty(context, result, "oldValue", old_value)) {
    *error = "V8 failed to populate a MutationRecord";
    return {};
  }
  return result;
}

v8::MaybeLocal<v8::Array> TakeMutationObserverRecords(
    MutationObserverState *state, v8::Local<v8::Context> context,
    std::string *error) {
  v8::Isolate *isolate = state->realm->isolate;
  if (state->records.size() >
      static_cast<size_t>(std::numeric_limits<int>::max())) {
    *error = "MutationObserver record queue exceeds V8 limits";
    return {};
  }
  v8::Local<v8::Array> records =
      v8::Array::New(isolate, static_cast<int>(state->records.size()));
  for (size_t index = 0; index < state->records.size(); ++index) {
    v8::Local<v8::Object> record;
    if (!BuildMutationRecordObject(state->realm, context,
                                   state->records[index], error)
             .ToLocal(&record) ||
        !records->Set(context, static_cast<uint32_t>(index), record)
             .FromMaybe(false)) {
      if (error->empty())
        *error = "V8 failed to populate MutationObserver records";
      return {};
    }
  }
  state->records.clear();
  return records;
}

bool ParseMutationObserverOptions(v8::Local<v8::Context> context,
                                  v8::Local<v8::Value> value,
                                  MutationObserverOptions *options) {
  if (value.IsEmpty() || !value->IsObject())
    return false;
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  v8::Local<v8::Object> object = value.As<v8::Object>();
  if (!ReadBooleanOption(context, object, "childList", false,
                         &options->child_list) ||
      !ReadBooleanOption(context, object, "attributes", false,
                         &options->attributes) ||
      !ReadBooleanOption(context, object, "characterData", false,
                         &options->character_data) ||
      !ReadBooleanOption(context, object, "subtree", false,
                         &options->subtree) ||
      !ReadBooleanOption(context, object, "attributeOldValue", false,
                         &options->attribute_old_value) ||
      !ReadBooleanOption(context, object, "characterDataOldValue", false,
                         &options->character_data_old_value))
    return false;
  v8::Local<v8::Value> filter;
  if (!object
           ->Get(context,
                 v8::String::NewFromUtf8Literal(isolate, "attributeFilter"))
           .ToLocal(&filter))
    return false;
  if (!filter->IsUndefined()) {
    if (!filter->IsArray())
      return false;
    v8::Local<v8::Array> values = filter.As<v8::Array>();
    for (uint32_t index = 0; index < values->Length(); ++index) {
      v8::Local<v8::Value> item;
      std::string name;
      if (!values->Get(context, index).ToLocal(&item) ||
          !StringFromValue(isolate, item, &name))
        return false;
      options->attribute_filter.insert(name);
    }
    options->attributes = true;
  }
  if (options->attribute_old_value)
    options->attributes = true;
  if (options->character_data_old_value)
    options->character_data = true;
  return options->child_list || options->attributes ||
         options->character_data;
}

void MutationObserverConstructor(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  if (!info.IsConstructCall() || info.Length() == 0 ||
      !info[0]->IsFunction()) {
    ThrowTypeError(isolate, "MutationObserver requires a callback");
    return;
  }
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  auto state = std::make_unique<MutationObserverState>();
  state->realm = realm;
  state->callback.Reset(isolate, info[0].As<v8::Function>());
  auto weak = std::make_unique<MutationObserverWeakData>();
  weak->realm = realm;
  weak->state = state.get();
  weak->object.Reset(isolate, info.This());
  state->weak = weak.get();
  info.This()->SetInternalField(
      kMutationObserverStateField,
      v8::External::New(isolate, state.get(),
                        v8::kExternalPointerTypeTagDefault));
  weak->object.SetWeak(weak.get(), MutationObserverCollected,
                       v8::WeakCallbackType::kParameter);
  realm->mutation_observers.insert(weak.get());
  state.release();
  weak.release();
  info.GetReturnValue().Set(info.This());
}

void MutationObserverObserve(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  MutationObserverState *state =
      ReadMutationObserverState(isolate, info.This());
  if (state == nullptr)
    return;
  WrapperKey target;
  if (info.Length() < 2 || !info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &target)) {
    ThrowTypeError(isolate, "observe requires a native Node target");
    return;
  }
  MutationObserverOptions options;
  if (!ParseMutationObserverOptions(isolate->GetCurrentContext(), info[1],
                                    &options)) {
    ThrowTypeError(isolate, "observe requires at least one mutation type");
    return;
  }
  std::string error;
  if (!state->registrations.empty() &&
      !FetchMutationObserverRecords(state, &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (state->registrations.empty() &&
      !CurrentMutationSequence(state->realm, &state->cursor, &error)) {
    ThrowError(isolate, error);
    return;
  }
  auto existing = std::find_if(
      state->registrations.begin(), state->registrations.end(),
      [&target](const MutationObserverRegistration &registration) {
        return registration.target == target;
      });
  if (existing == state->registrations.end()) {
    MutationObserverRegistration registration;
    registration.target = target;
    registration.options = std::move(options);
    registration.target_object.Reset(isolate, info[0].As<v8::Object>());
    state->registrations.push_back(std::move(registration));
  } else {
    existing->options = std::move(options);
    existing->target_object.Reset(isolate, info[0].As<v8::Object>());
  }
}

void MutationObserverDisconnect(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  MutationObserverState *state =
      ReadMutationObserverState(info.GetIsolate(), info.This());
  if (state == nullptr)
    return;
  state->registrations.clear();
  state->records.clear();
  std::string error;
  if (!CurrentMutationSequence(state->realm, &state->cursor, &error))
    ThrowError(info.GetIsolate(), error);
}

void MutationObserverTakeRecords(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  MutationObserverState *state =
      ReadMutationObserverState(isolate, info.This());
  if (state == nullptr)
    return;
  std::string error;
  if (!FetchMutationObserverRecords(state, &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::Array> records;
  if (!TakeMutationObserverRecords(state, isolate->GetCurrentContext(),
                                   &error)
           .ToLocal(&records)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(records);
}

bool DeliverMutationObservers(gossamer_v8_realm *realm,
                              v8::Local<v8::Context> context,
                              std::string *error) {
  for (size_t pass = 0; pass < 1024; ++pass) {
    bool delivered = false;
    std::vector<MutationObserverWeakData *> observers(
        realm->mutation_observers.begin(), realm->mutation_observers.end());
    for (MutationObserverWeakData *weak : observers) {
      if (weak == nullptr || weak->state == nullptr ||
          weak->state->registrations.empty())
        continue;
      MutationObserverState *state = weak->state;
      if (!FetchMutationObserverRecords(state, error))
        return false;
      if (state->records.empty())
        continue;
      v8::Local<v8::Array> records;
      if (!TakeMutationObserverRecords(state, context, error)
               .ToLocal(&records))
        return false;
      v8::Local<v8::Object> observer = weak->object.Get(realm->isolate);
      v8::Local<v8::Value> arguments[] = {records, observer};
      v8::TryCatch caught(realm->isolate);
      if (state->callback.Get(realm->isolate)
              ->Call(context, v8::Undefined(realm->isolate), 2, arguments)
              .IsEmpty()) {
        *error = DescribeException(realm->isolate, context, caught);
        return false;
      }
      delivered = true;
    }
    if (!delivered)
      return true;
  }
  *error = "MutationObserver delivery did not quiesce";
  return false;
}

RangeState *ReadRangeState(v8::Isolate *isolate,
                           v8::Local<v8::Object> receiver) {
  if (receiver.IsEmpty() || receiver->InternalFieldCount() != 1) {
    ThrowTypeError(isolate, "Range receiver is invalid");
    return nullptr;
  }
  v8::Local<v8::Data> data = receiver->GetInternalField(kRangeStateField);
  if (!data->IsValue() || !data.As<v8::Value>()->IsExternal()) {
    ThrowTypeError(isolate, "Range lost its native state");
    return nullptr;
  }
  return static_cast<RangeState *>(
      data.As<v8::Value>().As<v8::External>()->Value(
          v8::kExternalPointerTypeTagDefault));
}

void RangeCollected(const v8::WeakCallbackInfo<RangeWeakData> &info) {
  RangeWeakData *weak = info.GetParameter();
  if (weak == nullptr)
    return;
  if (weak->realm != nullptr)
    weak->realm->ranges.erase(weak);
  weak->object.Reset();
  if (weak->state != nullptr) {
    weak->state->start_object.Reset();
    weak->state->end_object.Reset();
    delete weak->state;
  }
  delete weak;
}

bool TrackRangeObject(gossamer_v8_realm *realm, v8::Local<v8::Object> object,
                      RangeState *state) {
  auto weak = std::make_unique<RangeWeakData>();
  weak->realm = realm;
  weak->state = state;
  weak->object.Reset(realm->isolate, object);
  state->weak = weak.get();
  object->SetInternalField(
      kRangeStateField,
      v8::External::New(realm->isolate, state,
                        v8::kExternalPointerTypeTagDefault));
  weak->object.SetWeak(weak.get(), RangeCollected,
                       v8::WeakCallbackType::kParameter);
  realm->ranges.insert(weak.get());
  weak.release();
  return true;
}

void RangeConstructor(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  if (!info.IsConstructCall()) {
    ThrowTypeError(isolate, "Range constructor must be called with new");
    return;
  }
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (!realm->document_bound) {
    ThrowError(isolate, "Range requires a bound document");
    return;
  }
  auto state = std::make_unique<RangeState>();
  state->realm = realm;
  state->start = state->end = realm->document_key;
  v8::Local<v8::Object> document = realm->document_wrapper.Get(isolate);
  state->start_object.Reset(isolate, document);
  state->end_object.Reset(isolate, document);
  TrackRangeObject(realm, info.This(), state.release());
  info.GetReturnValue().Set(info.This());
}

bool RangeParent(gossamer_v8_realm *realm, const WrapperKey &key,
                 WrapperKey *parent, bool *found, std::string *error) {
  uint32_t node = 0;
  int host_found = 0;
  char *host_error = nullptr;
  if (realm->active_host->related_node(
          realm->active_host->execution_id, key.document, key.node, 1, &node,
          &host_found, &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "reading Range parent failed";
    return false;
  }
  std::free(host_error);
  *found = host_found != 0;
  if (*found)
    *parent = WrapperKey{key.document, node};
  return true;
}

bool RangeBoundaryLimit(gossamer_v8_realm *realm, const WrapperKey &key,
                        uint32_t *limit, std::string *error) {
  NodeMetadata metadata;
  if (!ReadNodeMetadata(realm, key, &metadata, error))
    return false;
  if (metadata.type == 3 || metadata.type == 8 || metadata.type == 7) {
    char *value = nullptr;
    size_t length = 0;
    int non_null = 0;
    char *host_error = nullptr;
    if (realm->active_host->node_value(
            realm->active_host->execution_id, key.document, key.node, &value,
            &length, &non_null, &host_error) == 0) {
      *error = TakeCString(host_error);
      std::free(value);
      if (error->empty())
        *error = "reading Range character data failed";
      return false;
    }
    std::free(host_error);
    v8::Local<v8::String> rendered;
    if (!v8::String::NewFromUtf8(
             realm->isolate, value, v8::NewStringType::kNormal,
             static_cast<int>(std::min<size_t>(
                 length, static_cast<size_t>(std::numeric_limits<int>::max()))))
             .ToLocal(&rendered)) {
      std::free(value);
      *error = "decoding Range character data failed";
      return false;
    }
    std::free(value);
    *limit = static_cast<uint32_t>(rendered->Length());
    return true;
  }
  std::vector<uint32_t> children;
  if (!ReadChildNodes(realm, key, false, &children, error))
    return false;
  *limit = static_cast<uint32_t>(children.size());
  return true;
}

bool SetRangeBoundary(RangeState *state, bool start,
                      v8::Local<v8::Object> object, const WrapperKey &key,
                      uint32_t offset, std::string *error) {
  uint32_t limit = 0;
  if (!RangeBoundaryLimit(state->realm, key, &limit, error))
    return false;
  if (offset > limit) {
    *error = "Range boundary offset exceeds the node length";
    return false;
  }
  if (start) {
    state->start = key;
    state->start_offset = offset;
    state->start_object.Reset(state->realm->isolate, object);
  } else {
    state->end = key;
    state->end_offset = offset;
    state->end_object.Reset(state->realm->isolate, object);
  }
  return true;
}

void RangeSetBoundary(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  RangeState *state = ReadRangeState(isolate, info.This());
  WrapperKey key;
  uint32_t offset = 0;
  if (state == nullptr || info.Length() < 2 || !info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &key) ||
      !info[1]->Uint32Value(isolate->GetCurrentContext()).To(&offset)) {
    if (state != nullptr)
      ThrowTypeError(isolate, "Range boundary requires a native Node and offset");
    return;
  }
  std::string error;
  if (!SetRangeBoundary(state, info.Data()->IsTrue(),
                        info[0].As<v8::Object>(), key, offset, &error))
    ThrowDOMException(isolate, "IndexSizeError", error);
}

bool RangeNodeIndex(gossamer_v8_realm *realm, const WrapperKey &node,
                    WrapperKey *parent, uint32_t *index,
                    std::string *error) {
  bool found = false;
  if (!RangeParent(realm, node, parent, &found, error))
    return false;
  if (!found) {
    *error = "Range node has no parent";
    return false;
  }
  std::vector<uint32_t> children;
  if (!ReadChildNodes(realm, *parent, false, &children, error))
    return false;
  auto position = std::find(children.begin(), children.end(), node.node);
  if (position == children.end()) {
    *error = "Range node is missing from its parent";
    return false;
  }
  *index = static_cast<uint32_t>(position - children.begin());
  return true;
}

void RangeSelectNode(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  RangeState *state = ReadRangeState(isolate, info.This());
  WrapperKey node;
  if (state == nullptr || info.Length() == 0 || !info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &node)) {
    if (state != nullptr)
      ThrowTypeError(isolate, "selectNode requires a native Node");
    return;
  }
  WrapperKey parent;
  uint32_t index = 0;
  std::string error;
  if (!RangeNodeIndex(state->realm, node, &parent, &index, &error)) {
    ThrowDOMException(isolate, "InvalidNodeTypeError", error);
    return;
  }
  v8::Local<v8::Object> parent_wrapper;
  if (!GetOrCreateNodeWrapper(state->realm, isolate->GetCurrentContext(),
                              parent, &error)
           .ToLocal(&parent_wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  state->start = state->end = parent;
  state->start_offset = index;
  state->end_offset = index + 1;
  state->start_object.Reset(isolate, parent_wrapper);
  state->end_object.Reset(isolate, parent_wrapper);
}

void RangeSelectNodeContents(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  RangeState *state = ReadRangeState(isolate, info.This());
  WrapperKey node;
  if (state == nullptr || info.Length() == 0 || !info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &node)) {
    if (state != nullptr)
      ThrowTypeError(isolate, "selectNodeContents requires a native Node");
    return;
  }
  uint32_t limit = 0;
  std::string error;
  if (!RangeBoundaryLimit(state->realm, node, &limit, &error)) {
    ThrowError(isolate, error);
    return;
  }
  state->start = state->end = node;
  state->start_offset = 0;
  state->end_offset = limit;
  state->start_object.Reset(isolate, info[0].As<v8::Object>());
  state->end_object.Reset(isolate, info[0].As<v8::Object>());
}

void RangeCollapse(const v8::FunctionCallbackInfo<v8::Value> &info) {
  RangeState *state = ReadRangeState(info.GetIsolate(), info.This());
  if (state == nullptr)
    return;
  bool to_start = info.Length() != 0 && info[0]->BooleanValue(info.GetIsolate());
  if (to_start) {
    state->end = state->start;
    state->end_offset = state->start_offset;
    state->end_object.Reset(info.GetIsolate(),
                            state->start_object.Get(info.GetIsolate()));
  } else {
    state->start = state->end;
    state->start_offset = state->end_offset;
    state->start_object.Reset(info.GetIsolate(),
                              state->end_object.Get(info.GetIsolate()));
  }
}

bool RangeCommonAncestor(RangeState *state, WrapperKey *common,
                         std::string *error) {
  std::unordered_set<WrapperKey, WrapperKeyHash> start_ancestors;
  WrapperKey cursor = state->start;
  for (;;) {
    start_ancestors.insert(cursor);
    WrapperKey parent;
    bool found = false;
    if (!RangeParent(state->realm, cursor, &parent, &found, error))
      return false;
    if (!found)
      break;
    cursor = parent;
  }
  cursor = state->end;
  for (;;) {
    if (start_ancestors.find(cursor) != start_ancestors.end()) {
      *common = cursor;
      return true;
    }
    WrapperKey parent;
    bool found = false;
    if (!RangeParent(state->realm, cursor, &parent, &found, error))
      return false;
    if (!found)
      break;
    cursor = parent;
  }
  *error = "Range boundaries do not share a document tree";
  return false;
}

void RangePropertyFunctionGetter(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  RangeState *state = ReadRangeState(isolate, info.This());
  if (state == nullptr)
    return;
  int property = info.Data().As<v8::Int32>()->Value();
  if (property == 2) {
    info.GetReturnValue().Set(
        v8::Integer::NewFromUnsigned(isolate, state->start_offset));
    return;
  }
  if (property == 4) {
    info.GetReturnValue().Set(
        v8::Integer::NewFromUnsigned(isolate, state->end_offset));
    return;
  }
  if (property == 5) {
    info.GetReturnValue().Set(state->start == state->end &&
                              state->start_offset == state->end_offset);
    return;
  }
  WrapperKey key = property == 1 ? state->start : state->end;
  std::string error;
  if (property == 6 && !RangeCommonAncestor(state, &key, &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(state->realm, isolate->GetCurrentContext(), key,
                              &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

bool NewRangeObject(gossamer_v8_realm *realm,
                    v8::Local<v8::Context> context,
                    const RangeState &source, v8::Local<v8::Object> *result,
                    std::string *error) {
  if (!realm->range_template.Get(realm->isolate)
           ->InstanceTemplate()
           ->NewInstance(context)
           .ToLocal(result)) {
    *error = "V8 failed to allocate a Range";
    return false;
  }
  auto state = std::make_unique<RangeState>();
  state->realm = realm;
  state->start = source.start;
  state->end = source.end;
  state->start_offset = source.start_offset;
  state->end_offset = source.end_offset;
  state->start_object.Reset(realm->isolate,
                            source.start_object.Get(realm->isolate));
  state->end_object.Reset(realm->isolate,
                          source.end_object.Get(realm->isolate));
  TrackRangeObject(realm, *result, state.release());
  return true;
}

void RangeCloneRange(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  RangeState *state = ReadRangeState(isolate, info.This());
  if (state == nullptr)
    return;
  v8::Local<v8::Object> clone;
  std::string error;
  if (!NewRangeObject(state->realm, isolate->GetCurrentContext(), *state,
                      &clone, &error)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(clone);
}

bool CreateRangeFragment(RangeState *state, bool clone, bool remove,
                         v8::Local<v8::Context> context,
                         v8::Local<v8::Object> *fragment_wrapper,
                         std::string *error) {
  uint64_t fragment_document = 0;
  uint32_t fragment_node = 0;
  char *host_error = nullptr;
  uint8_t operation = clone ? 1 : (remove ? 3 : 2);
  if (state->realm->active_host->range_contents(
          state->realm->active_host->execution_id, state->start.document,
          state->start.node, static_cast<int32_t>(state->start_offset),
          state->end.document, state->end.node,
          static_cast<int32_t>(state->end_offset), operation,
          &fragment_document, &fragment_node, &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "applying Range contents failed";
    return false;
  }
  std::free(host_error);
  if (!clone) {
    state->end = state->start;
    state->end_offset = state->start_offset;
    state->end_object.Reset(state->realm->isolate,
                            state->start_object.Get(state->realm->isolate));
  }
  if (!remove) {
    if (!GetOrCreateNodeWrapper(
             state->realm, context,
             WrapperKey{fragment_document, fragment_node}, error)
             .ToLocal(fragment_wrapper))
      return false;
  }
  return true;
}

void RangeContents(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  RangeState *state = ReadRangeState(isolate, info.This());
  if (state == nullptr)
    return;
  int operation = info.Data().As<v8::Int32>()->Value();
  v8::Local<v8::Object> fragment;
  std::string error;
  bool clone = operation == 1;
  bool remove = operation == 3;
  if (!CreateRangeFragment(state, clone, remove,
                           isolate->GetCurrentContext(), &fragment, &error)) {
    ThrowDOMException(isolate, "InvalidStateError", error);
    return;
  }
  if (operation != 3)
    info.GetReturnValue().Set(fragment);
}

void RangeInsertNode(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  RangeState *state = ReadRangeState(isolate, info.This());
  WrapperKey node;
  if (state == nullptr || info.Length() == 0 || !info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &node)) {
    if (state != nullptr)
      ThrowTypeError(isolate, "insertNode requires a native Node");
    return;
  }
  std::string error;
  NodeMetadata metadata;
  if (!ReadNodeMetadata(state->realm, state->start, &metadata, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  int ok = 0;
  if (metadata.type == 3) {
    uint64_t tail_document = 0;
    uint32_t tail_node = 0;
    if (state->realm->active_host->split_text(
            state->realm->active_host->execution_id, state->start.document,
            state->start.node, static_cast<int32_t>(state->start_offset),
            &tail_document, &tail_node, &host_error) == 0) {
      error = TakeCString(host_error);
      ThrowError(isolate, error.empty() ? "Range text split failed" : error);
      return;
    }
    std::free(host_error);
    WrapperKey parent;
    bool found = false;
    if (!RangeParent(state->realm, WrapperKey{tail_document, tail_node},
                     &parent, &found, &error) ||
        !found) {
      ThrowDOMException(isolate, "HierarchyRequestError",
                        error.empty() ? "Range text boundary has no parent"
                                      : error);
      return;
    }
    host_error = nullptr;
    ok = state->realm->active_host->insert_before(
        state->realm->active_host->execution_id, parent.document, parent.node,
        node.document, node.node, tail_document, tail_node, &host_error);
  } else {
    std::vector<uint32_t> children;
    if (!ReadChildNodes(state->realm, state->start, false, &children, &error)) {
      ThrowError(isolate, error);
      return;
    }
    ok = state->start_offset < children.size()
             ? state->realm->active_host->insert_before(
                   state->realm->active_host->execution_id,
                   state->start.document, state->start.node, node.document,
                   node.node, state->start.document,
                   children[state->start_offset], &host_error)
             : state->realm->active_host->append_child(
                   state->realm->active_host->execution_id,
                   state->start.document, state->start.node, node.document,
                   node.node, &host_error);
  }
  if (ok == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "Range insertNode failed" : error);
    return;
  }
  std::free(host_error);
}

void RangeDetach(const v8::FunctionCallbackInfo<v8::Value> &) {}

SelectionState *ReadSelectionState(v8::Isolate *isolate,
                                   v8::Local<v8::Object> receiver) {
  if (receiver.IsEmpty() || receiver->InternalFieldCount() != 1) {
    ThrowTypeError(isolate, "Selection receiver is invalid");
    return nullptr;
  }
  v8::Local<v8::Data> data = receiver->GetInternalField(kSelectionStateField);
  if (!data->IsValue() || !data.As<v8::Value>()->IsExternal()) {
    ThrowTypeError(isolate, "Selection lost its native state");
    return nullptr;
  }
  return static_cast<SelectionState *>(
      data.As<v8::Value>().As<v8::External>()->Value(
          v8::kExternalPointerTypeTagDefault));
}

RangeState *SelectionRange(SelectionState *selection) {
  if (selection == nullptr || selection->range_object.IsEmpty())
    return nullptr;
  v8::Local<v8::Object> object =
      selection->range_object.Get(selection->realm->isolate);
  if (object.IsEmpty() || object->InternalFieldCount() != 1)
    return nullptr;
  v8::Local<v8::Data> data = object->GetInternalField(kRangeStateField);
  if (!data->IsValue() || !data.As<v8::Value>()->IsExternal())
    return nullptr;
  return static_cast<RangeState *>(
      data.As<v8::Value>().As<v8::External>()->Value(
          v8::kExternalPointerTypeTagDefault));
}

bool SetSelectionRange(SelectionState *selection, const RangeState &source,
                       v8::Local<v8::Context> context, std::string *error) {
  v8::Local<v8::Object> range;
  if (!NewRangeObject(selection->realm, context, source, &range, error))
    return false;
  selection->range_object.Reset(selection->realm->isolate, range);
  return true;
}

void SelectionPropertyFunctionGetter(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  SelectionState *selection = ReadSelectionState(isolate, info.This());
  if (selection == nullptr)
    return;
  int property = info.Data().As<v8::Int32>()->Value();
  RangeState *range = SelectionRange(selection);
  if (property == 6) {
    info.GetReturnValue().Set(range == nullptr ? 0 : 1);
    return;
  }
  if (property == 5) {
    info.GetReturnValue().Set(
        range == nullptr ||
        (range->start == range->end &&
         range->start_offset == range->end_offset));
    return;
  }
  if (property == 7) {
    const char *type = range == nullptr
                           ? "None"
                           : (range->start == range->end &&
                                      range->start_offset == range->end_offset
                                  ? "Caret"
                                  : "Range");
    info.GetReturnValue().Set(
        v8::String::NewFromUtf8(isolate, type).ToLocalChecked());
    return;
  }
  if (property == 2 || property == 4) {
    uint32_t offset =
        range == nullptr ? 0
                         : (property == 2 ? range->start_offset
                                          : range->end_offset);
    info.GetReturnValue().Set(v8::Integer::NewFromUnsigned(isolate, offset));
    return;
  }
  if (range == nullptr) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  WrapperKey key = property == 1 ? range->start : range->end;
  std::string error;
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(selection->realm, isolate->GetCurrentContext(),
                              key, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void SelectionGetRangeAt(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  SelectionState *selection = ReadSelectionState(isolate, info.This());
  uint32_t index = 0;
  if (selection == nullptr || info.Length() == 0 ||
      !info[0]->Uint32Value(isolate->GetCurrentContext()).To(&index))
    return;
  if (index != 0 || selection->range_object.IsEmpty()) {
    ThrowDOMException(isolate, "IndexSizeError",
                      "Selection range index is out of bounds");
    return;
  }
  info.GetReturnValue().Set(selection->range_object.Get(isolate));
}

void SelectionAddRange(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  SelectionState *selection = ReadSelectionState(isolate, info.This());
  if (selection == nullptr || info.Length() == 0 || !info[0]->IsObject()) {
    if (selection != nullptr)
      ThrowTypeError(isolate, "addRange requires a native Range");
    return;
  }
  v8::Local<v8::Object> object = info[0].As<v8::Object>();
  if (!selection->realm->range_template.Get(isolate)->HasInstance(object) ||
      object->InternalFieldCount() != 1 ||
      !object->GetInternalField(kRangeStateField)->IsValue() ||
      !object->GetInternalField(kRangeStateField)
           .As<v8::Value>()
           ->IsExternal()) {
    ThrowTypeError(isolate, "addRange requires a native Range");
    return;
  }
  selection->range_object.Reset(isolate, object);
}

void SelectionRemoveAllRanges(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  SelectionState *selection =
      ReadSelectionState(info.GetIsolate(), info.This());
  if (selection != nullptr)
    selection->range_object.Reset();
}

void SelectionCollapse(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  SelectionState *selection = ReadSelectionState(isolate, info.This());
  if (selection == nullptr)
    return;
  if (info.Length() == 0 || info[0]->IsNull()) {
    selection->range_object.Reset();
    return;
  }
  WrapperKey key;
  uint32_t offset = 0;
  if (!info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &key) ||
      (info.Length() > 1 &&
       !info[1]->Uint32Value(isolate->GetCurrentContext()).To(&offset))) {
    ThrowTypeError(isolate, "collapse requires a native Node and offset");
    return;
  }
  RangeState source;
  source.realm = selection->realm;
  std::string error;
  if (!SetRangeBoundary(&source, true, info[0].As<v8::Object>(), key, offset,
                        &error) ||
      !SetRangeBoundary(&source, false, info[0].As<v8::Object>(), key, offset,
                        &error) ||
      !SetSelectionRange(selection, source, isolate->GetCurrentContext(),
                         &error)) {
    ThrowDOMException(isolate, "IndexSizeError", error);
  }
}

void SelectionCollapseToEdge(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  SelectionState *selection = ReadSelectionState(isolate, info.This());
  RangeState *range = SelectionRange(selection);
  if (selection == nullptr || range == nullptr) {
    if (selection != nullptr)
      ThrowDOMException(isolate, "InvalidStateError", "Selection is empty");
    return;
  }
  bool to_start = info.Data()->IsTrue();
  if (to_start) {
    range->end = range->start;
    range->end_offset = range->start_offset;
    range->end_object.Reset(isolate, range->start_object.Get(isolate));
  } else {
    range->start = range->end;
    range->start_offset = range->end_offset;
    range->start_object.Reset(isolate, range->end_object.Get(isolate));
  }
}

void SelectionSelectAllChildren(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  SelectionState *selection = ReadSelectionState(isolate, info.This());
  WrapperKey key;
  if (selection == nullptr || info.Length() == 0 || !info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &key)) {
    if (selection != nullptr)
      ThrowTypeError(isolate, "selectAllChildren requires a native Node");
    return;
  }
  uint32_t limit = 0;
  std::string error;
  if (!RangeBoundaryLimit(selection->realm, key, &limit, &error)) {
    ThrowError(isolate, error);
    return;
  }
  RangeState source;
  source.realm = selection->realm;
  source.start = source.end = key;
  source.start_offset = 0;
  source.end_offset = limit;
  source.start_object.Reset(isolate, info[0].As<v8::Object>());
  source.end_object.Reset(isolate, info[0].As<v8::Object>());
  if (!SetSelectionRange(selection, source, isolate->GetCurrentContext(),
                         &error))
    ThrowError(isolate, error);
}

void SelectionDeleteFromDocument(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  SelectionState *selection =
      ReadSelectionState(info.GetIsolate(), info.This());
  RangeState *range = SelectionRange(selection);
  if (selection == nullptr || range == nullptr)
    return;
  v8::Local<v8::Object> unused;
  std::string error;
  if (!CreateRangeFragment(range, false, true,
                           info.GetIsolate()->GetCurrentContext(), &unused,
                           &error))
    ThrowDOMException(info.GetIsolate(), "InvalidStateError", error);
}

void SelectionToString(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  SelectionState *selection = ReadSelectionState(isolate, info.This());
  RangeState *range = SelectionRange(selection);
  if (selection == nullptr || range == nullptr) {
    info.GetReturnValue().Set(v8::String::Empty(isolate));
    return;
  }
  v8::Local<v8::Object> fragment;
  std::string error;
  if (!CreateRangeFragment(range, true, false, isolate->GetCurrentContext(),
                           &fragment, &error)) {
    ThrowDOMException(isolate, "InvalidStateError", error);
    return;
  }
  WrapperKey key;
  if (!ReadWrapperKey(fragment, &key))
    return;
  char *value = nullptr;
  size_t length = 0;
  char *host_error = nullptr;
  if (selection->realm->active_host->text_content(
          selection->realm->active_host->execution_id, key.document, key.node,
          &value, &length, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(value);
    ThrowError(isolate, error.empty() ? "reading Selection text failed"
                                      : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> rendered;
  if (v8::String::NewFromUtf8(
          isolate, value, v8::NewStringType::kNormal,
          static_cast<int>(std::min<size_t>(
              length, static_cast<size_t>(std::numeric_limits<int>::max()))))
          .ToLocal(&rendered))
    info.GetReturnValue().Set(rendered);
  std::free(value);
}

void GetSelection(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (realm->selection_object.IsEmpty()) {
    v8::Local<v8::Object> object;
    if (!realm->selection_template.Get(isolate)
             ->InstanceTemplate()
             ->NewInstance(isolate->GetCurrentContext())
             .ToLocal(&object)) {
      ThrowError(isolate, "V8 failed to allocate a Selection");
      return;
    }
    auto state = std::make_unique<SelectionState>();
    state->realm = realm;
    object->SetInternalField(
        kSelectionStateField,
        v8::External::New(isolate, state.get(),
                          v8::kExternalPointerTypeTagDefault));
    realm->selection_state = state.release();
    realm->selection_object.Reset(isolate, object);
  }
  info.GetReturnValue().Set(realm->selection_object.Get(isolate));
}

void DocumentCreateRange(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  v8::Local<v8::Function> constructor;
  v8::Local<v8::Object> range;
  if (!realm->range_template.Get(isolate)
           ->GetFunction(isolate->GetCurrentContext())
           .ToLocal(&constructor) ||
      !constructor->NewInstance(isolate->GetCurrentContext(), 0, nullptr)
           .ToLocal(&range)) {
    ThrowError(isolate, "V8 failed to create a Range");
    return;
  }
  info.GetReturnValue().Set(range);
}

TraversalState *ReadTraversalState(v8::Isolate *isolate,
                                   v8::Local<v8::Object> receiver) {
  if (receiver.IsEmpty() || receiver->InternalFieldCount() != 1) {
    ThrowTypeError(isolate, "DOM traversal receiver is invalid");
    return nullptr;
  }
  v8::Local<v8::Data> data =
      receiver->GetInternalField(kTraversalStateField);
  if (!data->IsValue() || !data.As<v8::Value>()->IsExternal()) {
    ThrowTypeError(isolate, "DOM traversal object lost its native state");
    return nullptr;
  }
  return static_cast<TraversalState *>(
      data.As<v8::Value>().As<v8::External>()->Value(
          v8::kExternalPointerTypeTagDefault));
}

void TraversalCollected(const v8::WeakCallbackInfo<TraversalWeakData> &info) {
  TraversalWeakData *weak = info.GetParameter();
  if (weak == nullptr)
    return;
  if (weak->realm != nullptr)
    weak->realm->traversals.erase(weak);
  weak->object.Reset();
  if (weak->state != nullptr) {
    weak->state->root_object.Reset();
    weak->state->filter.Reset();
    delete weak->state;
  }
  delete weak;
}

bool TraversalAccepts(TraversalState *state, const WrapperKey &key,
                      v8::Local<v8::Context> context, int32_t *decision,
                      std::string *error) {
  NodeMetadata metadata;
  if (!ReadNodeMetadata(state->realm, key, &metadata, error))
    return false;
  if (metadata.type == 0 || metadata.type > 32 ||
      (state->what_to_show & (1u << (metadata.type - 1))) == 0) {
    *decision = 3;
    return true;
  }
  if (state->filter.IsEmpty() ||
      state->filter.Get(state->realm->isolate)->IsNull() ||
      state->filter.Get(state->realm->isolate)->IsUndefined()) {
    *decision = 1;
    return true;
  }
  v8::Local<v8::Value> filter = state->filter.Get(state->realm->isolate);
  v8::Local<v8::Function> function;
  v8::Local<v8::Value> receiver = v8::Undefined(state->realm->isolate);
  if (filter->IsFunction()) {
    function = filter.As<v8::Function>();
  } else if (filter->IsObject()) {
    receiver = filter;
    v8::Local<v8::Value> method;
    if (!filter.As<v8::Object>()
             ->Get(context, v8::String::NewFromUtf8Literal(
                                state->realm->isolate, "acceptNode"))
             .ToLocal(&method) ||
        !method->IsFunction()) {
      *error = "NodeFilter object requires acceptNode()";
      return false;
    }
    function = method.As<v8::Function>();
  } else {
    *error = "NodeFilter is not callable";
    return false;
  }
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(state->realm, context, key, error)
           .ToLocal(&wrapper))
    return false;
  v8::Local<v8::Value> arguments[] = {wrapper};
  v8::Local<v8::Value> result;
  if (!function->Call(context, receiver, 1, arguments).ToLocal(&result))
    return false;
  int32_t result_decision = 0;
  if (!result->Int32Value(context).To(&result_decision))
    return false;
  *decision = result_decision == 1 || result_decision == 2 ||
                      result_decision == 3
                  ? result_decision
                  : 3;
  return true;
}

bool BuildTraversalNodes(TraversalState *state,
                         v8::Local<v8::Context> context,
                         std::string *error) {
  state->nodes.clear();
  state->nodes.push_back(state->root);
  state->root_accepted = true;
  if (state->node_iterator) {
    int32_t root_decision = 3;
    if (!TraversalAccepts(state, state->root, context, &root_decision, error))
      return false;
    state->root_accepted = root_decision == 1;
  }
  std::function<bool(const WrapperKey &)> visit =
      [&](const WrapperKey &current) {
        int32_t decision = 3;
        if (!TraversalAccepts(state, current, context, &decision, error))
          return false;
        if (decision == 1)
          state->nodes.push_back(current);
        if (decision == 2 && !state->node_iterator)
          return true;
        std::vector<uint32_t> children;
        if (!ReadChildNodes(state->realm, current, false, &children, error))
          return false;
        for (uint32_t child : children) {
          if (!visit(WrapperKey{current.document, child}))
            return false;
        }
        return true;
      };
  std::vector<uint32_t> root_children;
  if (!ReadChildNodes(state->realm, state->root, false, &root_children,
                      error))
    return false;
  for (uint32_t child : root_children) {
    if (!visit(WrapperKey{state->root.document, child}))
      return false;
  }
  return CurrentMutationSequence(state->realm, &state->mutation_sequence,
                                 error);
}

bool RefreshTraversalNodes(TraversalState *state,
                           v8::Local<v8::Context> context,
                           std::string *error) {
  uint64_t sequence = 0;
  if (!CurrentMutationSequence(state->realm, &sequence, error))
    return false;
  if (sequence == state->mutation_sequence)
    return true;
  std::vector<WrapperKey> old_nodes = state->nodes;
  size_t old_index = state->index;
  WrapperKey current = old_nodes.empty()
                           ? state->root
                           : old_nodes[std::min(old_index, old_nodes.size() - 1)];
  if (!BuildTraversalNodes(state, context, error))
    return false;
  auto retained = std::find(state->nodes.begin(), state->nodes.end(), current);
  if (retained != state->nodes.end()) {
    state->index = static_cast<size_t>(retained - state->nodes.begin());
    return true;
  }
  for (size_t cursor = std::min(old_index, old_nodes.size()); cursor > 0;
       --cursor) {
    retained = std::find(state->nodes.begin(), state->nodes.end(),
                         old_nodes[cursor - 1]);
    if (retained != state->nodes.end()) {
      state->index = static_cast<size_t>(retained - state->nodes.begin());
      return true;
    }
  }
  state->index = 0;
  state->pointer_before_reference = true;
  return true;
}

bool TrackTraversalObject(gossamer_v8_realm *realm,
                          v8::Local<v8::Object> object,
                          TraversalState *state) {
  auto weak = std::make_unique<TraversalWeakData>();
  weak->realm = realm;
  weak->state = state;
  weak->object.Reset(realm->isolate, object);
  state->weak = weak.get();
  object->SetInternalField(
      kTraversalStateField,
      v8::External::New(realm->isolate, state,
                        v8::kExternalPointerTypeTagDefault));
  weak->object.SetWeak(weak.get(), TraversalCollected,
                       v8::WeakCallbackType::kParameter);
  realm->traversals.insert(weak.get());
  weak.release();
  return true;
}

void DocumentCreateTraversal(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey root;
  if (info.Length() == 0 || !info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &root)) {
    ThrowTypeError(isolate, "DOM traversal requires a native root Node");
    return;
  }
  auto state = std::make_unique<TraversalState>();
  state->realm = CurrentRealm(isolate);
  state->root = root;
  state->node_iterator = info.Data()->IsTrue();
  state->root_object.Reset(isolate, info[0].As<v8::Object>());
  if (info.Length() > 1 &&
      !info[1]->Uint32Value(isolate->GetCurrentContext())
           .To(&state->what_to_show))
    return;
  if (info.Length() > 2 && !info[2]->IsNull() && !info[2]->IsUndefined())
    state->filter.Reset(isolate, info[2]);
  std::string error;
  if (!BuildTraversalNodes(state.get(), isolate->GetCurrentContext(),
                           &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::FunctionTemplate> traversal_template =
      state->node_iterator
          ? state->realm->node_iterator_template.Get(isolate)
          : state->realm->tree_walker_template.Get(isolate);
  v8::Local<v8::Object> result;
  if (!traversal_template->InstanceTemplate()
           ->NewInstance(isolate->GetCurrentContext())
           .ToLocal(&result)) {
    ThrowError(isolate, "V8 failed to allocate a DOM traversal object");
    return;
  }
  TrackTraversalObject(state->realm, result, state.release());
  info.GetReturnValue().Set(result);
}

void TraversalPropertyFunctionGetter(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  TraversalState *state = ReadTraversalState(isolate, info.This());
  if (state == nullptr)
    return;
  std::string error;
  if (!RefreshTraversalNodes(state, isolate->GetCurrentContext(), &error)) {
    if (!isolate->HasPendingException())
      ThrowError(isolate, error);
    return;
  }
  int property = info.Data().As<v8::Int32>()->Value();
  if (property == 3) {
    info.GetReturnValue().Set(state->what_to_show);
    return;
  }
  if (property == 4) {
    info.GetReturnValue().Set(state->filter.IsEmpty()
                                  ? v8::Null(isolate)
                                  : state->filter.Get(isolate));
    return;
  }
  if (property == 6) {
    info.GetReturnValue().Set(state->pointer_before_reference);
    return;
  }
  WrapperKey key = property == 1 ? state->root : state->nodes[state->index];
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(state->realm, isolate->GetCurrentContext(), key,
                              &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void TreeWalkerCurrentNodeSetter(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  TraversalState *state = ReadTraversalState(isolate, info.This());
  WrapperKey key;
  if (state == nullptr || info.Length() == 0 || !info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &key))
    return;
  std::string error;
  if (!RefreshTraversalNodes(state, isolate->GetCurrentContext(), &error)) {
    if (!isolate->HasPendingException())
      ThrowError(isolate, error);
    return;
  }
  auto found = std::find(state->nodes.begin(), state->nodes.end(), key);
  if (found == state->nodes.end()) {
    ThrowDOMException(isolate, "NotSupportedError",
                      "currentNode must be inside the TreeWalker root");
    return;
  }
  state->index = static_cast<size_t>(found - state->nodes.begin());
}

void TraversalStep(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  TraversalState *state = ReadTraversalState(isolate, info.This());
  if (state == nullptr || state->nodes.empty())
    return;
  std::string error;
  if (!RefreshTraversalNodes(state, isolate->GetCurrentContext(), &error)) {
    if (!isolate->HasPendingException())
      ThrowError(isolate, error);
    return;
  }
  bool forward = info.Data()->IsTrue();
  if (state->node_iterator) {
    if (forward) {
      if (state->pointer_before_reference) {
        state->pointer_before_reference = false;
        if (state->index == 0 && !state->root_accepted) {
          if (state->nodes.size() == 1) {
            info.GetReturnValue().Set(v8::Null(isolate));
            return;
          }
          state->index = 1;
        }
      } else if (state->index + 1 < state->nodes.size()) {
        state->index++;
      } else {
        info.GetReturnValue().Set(v8::Null(isolate));
        return;
      }
    } else {
      if (!state->pointer_before_reference) {
        state->pointer_before_reference = true;
      } else if (state->index > 0) {
        state->index--;
      } else {
        info.GetReturnValue().Set(v8::Null(isolate));
        return;
      }
    }
  } else if (forward) {
    if (state->index + 1 >= state->nodes.size()) {
      info.GetReturnValue().Set(v8::Null(isolate));
      return;
    }
    state->index++;
  } else {
    if (state->index == 0) {
      info.GetReturnValue().Set(v8::Null(isolate));
      return;
    }
    state->index--;
  }
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(state->realm, isolate->GetCurrentContext(),
                              state->nodes[state->index], &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

bool LogicalTraversalParent(TraversalState *state, const WrapperKey &node,
                            WrapperKey *parent, bool *found,
                            std::string *error) {
  *found = false;
  if (node == state->root)
    return true;
  WrapperKey cursor = node;
  for (;;) {
    WrapperKey candidate;
    bool parent_found = false;
    if (!RangeParent(state->realm, cursor, &candidate, &parent_found, error))
      return false;
    if (!parent_found)
      return true;
    if (candidate == state->root ||
        std::find(state->nodes.begin(), state->nodes.end(), candidate) !=
            state->nodes.end()) {
      *parent = candidate;
      *found = true;
      return true;
    }
    cursor = candidate;
  }
}

void TreeWalkerRelation(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  TraversalState *state = ReadTraversalState(isolate, info.This());
  if (state == nullptr)
    return;
  std::string error;
  if (!RefreshTraversalNodes(state, isolate->GetCurrentContext(), &error)) {
    if (!isolate->HasPendingException())
      ThrowError(isolate, error);
    return;
  }
  int relation = info.Data().As<v8::Int32>()->Value();
  WrapperKey current = state->nodes[state->index];
  WrapperKey candidate;
  bool found = false;
  if (relation == 1) {
    if (!LogicalTraversalParent(state, current, &candidate, &found, &error)) {
      ThrowError(isolate, error);
      return;
    }
  } else if (relation == 2 || relation == 3) {
    for (const WrapperKey &node : state->nodes) {
      if (node == current)
        continue;
      WrapperKey logical_parent;
      bool parent_found = false;
      if (!LogicalTraversalParent(state, node, &logical_parent, &parent_found,
                                  &error)) {
        ThrowError(isolate, error);
        return;
      }
      if (parent_found && logical_parent == current) {
        candidate = node;
        found = true;
        if (relation == 2)
          break;
      }
    }
  } else {
    WrapperKey logical_parent;
    bool parent_found = false;
    if (!LogicalTraversalParent(state, current, &logical_parent, &parent_found,
                                &error)) {
      ThrowError(isolate, error);
      return;
    }
    WrapperKey previous;
    bool previous_found = false;
    bool saw_current = false;
    if (parent_found) {
      for (const WrapperKey &node : state->nodes) {
        WrapperKey node_parent;
        bool node_parent_found = false;
        if (!LogicalTraversalParent(state, node, &node_parent,
                                    &node_parent_found, &error)) {
          ThrowError(isolate, error);
          return;
        }
        if (!node_parent_found || node_parent != logical_parent)
          continue;
        if (node == current) {
          if (relation == 4 && previous_found) {
            candidate = previous;
            found = true;
          }
          saw_current = true;
          continue;
        }
        if (relation == 5 && saw_current) {
          candidate = node;
          found = true;
          break;
        }
        if (!saw_current) {
          previous = node;
          previous_found = true;
        }
      }
    }
  }
  if (!found) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  auto position = std::find(state->nodes.begin(), state->nodes.end(), candidate);
  if (position == state->nodes.end()) {
    info.GetReturnValue().Set(v8::Null(isolate));
    return;
  }
  state->index = static_cast<size_t>(position - state->nodes.begin());
  v8::Local<v8::Object> wrapper;
  if (!GetOrCreateNodeWrapper(state->realm, isolate->GetCurrentContext(),
                              candidate, &error)
           .ToLocal(&wrapper)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(wrapper);
}

void NodeIteratorDetach(const v8::FunctionCallbackInfo<v8::Value> &) {}

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
  } else if (name == "persisted") {
    info.GetReturnValue().Set(state->persisted);
  } else if (name == "state") {
    v8::Local<v8::String> encoded;
    v8::Local<v8::Value> value;
    const std::string &json = state->data.empty() ? std::string("null")
                                                  : state->data;
    if (!NewUTF8String(isolate, json.data(), json.size(), &encoded) ||
        !v8::JSON::Parse(isolate->GetCurrentContext(), encoded)
             .ToLocal(&value)) {
      ThrowError(isolate, "V8 failed to decode PopStateEvent.state");
      return;
    }
    info.GetReturnValue().Set(value);
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
             name == "data" || name == "inputType" || name == "oldURL" ||
             name == "newURL") {
    const std::string *value = &state->pointer_type;
    if (name == "key")
      value = &state->key;
    else if (name == "code")
      value = &state->code;
    else if (name == "data")
      value = &state->data;
    else if (name == "inputType")
      value = &state->input_type;
    else if (name == "oldURL")
      value = &state->key;
    else if (name == "newURL")
      value = &state->code;
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
    if (key.node == 0 && realm->document_bound &&
        key.document == realm->document_key.document) {
      info.GetReturnValue().Set(isolate->GetCurrentContext()->Global());
      return;
    }
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
      if (state->path[index].node == 0 && realm->document_bound &&
          state->path[index].document == realm->document_key.document) {
        if (!result
                 ->Set(context, static_cast<uint32_t>(index),
                       context->Global())
                 .FromMaybe(false)) {
          ThrowError(isolate, "V8 failed to build composedPath");
          return;
        }
        continue;
      }
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

void AddEventListenerForTarget(
    const v8::FunctionCallbackInfo<v8::Value> &info, const WrapperKey &key) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
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

void NodeAddEventListener(const v8::FunctionCallbackInfo<v8::Value> &info) {
  WrapperKey key;
  if (!ReadReceiverKey(info.GetIsolate(), info.This(), &key))
    return;
  AddEventListenerForTarget(info, key);
}

void WindowAddEventListener(const v8::FunctionCallbackInfo<v8::Value> &info) {
  gossamer_v8_realm *realm = CurrentRealm(info.GetIsolate());
  if (!realm->document_bound) {
    ThrowError(info.GetIsolate(), "Window has no bound document");
    return;
  }
  AddEventListenerForTarget(
      info, WrapperKey{realm->document_key.document, 0});
}

void RemoveEventListenerForTarget(
    const v8::FunctionCallbackInfo<v8::Value> &info, const WrapperKey &key) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
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

void NodeRemoveEventListener(const v8::FunctionCallbackInfo<v8::Value> &info) {
  WrapperKey key;
  if (!ReadReceiverKey(info.GetIsolate(), info.This(), &key))
    return;
  RemoveEventListenerForTarget(info, key);
}

void WindowRemoveEventListener(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  gossamer_v8_realm *realm = CurrentRealm(info.GetIsolate());
  if (!realm->document_bound)
    return;
  RemoveEventListenerForTarget(
      info, WrapperKey{realm->document_key.document, 0});
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
    if (current.node == 0)
      return true;
    WrapperKey parent;
    bool found = false;
    if (!ReadEventParent(realm, current, &parent, &found, error))
      return false;
    if (!found) {
      if (realm->document_bound && current == realm->document_key)
        path->push_back(WrapperKey{current.document, 0});
      return true;
    }
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
  if (target.node == 0 && realm->document_bound &&
      target.document == realm->document_key.document) {
    current_target = context->Global();
  } else if (!GetOrCreateNodeWrapper(realm, context, target, &wrapper_error)
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

void WindowDispatchEvent(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (!realm->document_bound) {
    ThrowError(isolate, "Window has no bound document");
    return;
  }
  if (info.Length() == 0 || !info[0]->IsObject()) {
    ThrowError(isolate, "dispatchEvent requires an Event");
    return;
  }
  v8::Local<v8::Object> event_object = info[0].As<v8::Object>();
  EventState *state = ReadEventState(isolate, event_object);
  if (state == nullptr)
    return;
  std::string error;
  if (!DispatchEventState(
          realm, isolate->GetCurrentContext(),
          WrapperKey{realm->document_key.document, 0}, event_object, state,
          &error)) {
    ThrowError(isolate, error);
    return;
  }
  info.GetReturnValue().Set(!state->default_prevented);
}

v8::Local<v8::FunctionTemplate>
EventTemplateForInterface(gossamer_v8_realm *realm,
                          EventInterface interface) {
  switch (interface) {
  case EventInterface::CustomEvent:
    return realm->custom_event_template.Get(realm->isolate);
  case EventInterface::MouseEvent:
    return realm->mouse_event_template.Get(realm->isolate);
  case EventInterface::PointerEvent:
    return realm->pointer_event_template.Get(realm->isolate);
  case EventInterface::KeyboardEvent:
    return realm->keyboard_event_template.Get(realm->isolate);
  case EventInterface::InputEvent:
    return realm->input_event_template.Get(realm->isolate);
  case EventInterface::CompositionEvent:
    return realm->composition_event_template.Get(realm->isolate);
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

bool ReadHostFormValidity(gossamer_v8_realm *realm, const WrapperKey &form,
                          bool *valid, std::vector<uint32_t> *invalid,
                          std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  int host_valid = 0;
  uint32_t *host_invalid = nullptr;
  size_t count = 0;
  char *host_error = nullptr;
  if (realm->active_host->form_validity(
          realm->active_host->execution_id, form.document, form.node,
          &host_valid, &host_invalid, &count, &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(host_invalid);
    return false;
  }
  std::free(host_error);
  invalid->assign(host_invalid, host_invalid + count);
  std::free(host_invalid);
  *valid = host_valid != 0;
  return true;
}

bool DispatchFormEvent(gossamer_v8_realm *realm,
                       v8::Local<v8::Context> context,
                       const WrapperKey &target, const char *type,
                       bool bubbles, const WrapperKey *submitter,
                       bool *default_prevented, std::string *error) {
  auto state = std::make_unique<EventState>();
  state->interface = EventInterface::Event;
  state->type = type;
  state->bubbles = bubbles;
  state->cancelable = true;
  state->timestamp = static_cast<double>(MonotonicNanos()) / 1000000.0;
  EventState *raw_state = state.get();
  v8::Local<v8::Object> event_object;
  if (!NewEventObject(realm, context, std::move(state)).ToLocal(&event_object)) {
    *error = "V8 failed to allocate a form event";
    return false;
  }
  if (submitter != nullptr && submitter->node != 0) {
    v8::Local<v8::Object> wrapper;
    if (!GetOrCreateNodeWrapper(realm, context, *submitter, error)
             .ToLocal(&wrapper) ||
        !event_object
             ->Set(context,
                   v8::String::NewFromUtf8Literal(realm->isolate,
                                                  "submitter"),
                   wrapper)
             .FromMaybe(false)) {
      if (error->empty())
        *error = "V8 failed to expose the form submitter";
      return false;
    }
  }
  if (!DispatchEventState(realm, context, target, event_object, raw_state,
                          error))
    return false;
  *default_prevented = raw_state->default_prevented;
  return true;
}

bool DispatchInvalidControls(gossamer_v8_realm *realm,
                             v8::Local<v8::Context> context,
                             uint64_t document,
                             const std::vector<uint32_t> &invalid,
                             std::string *error) {
  for (uint32_t node : invalid) {
    bool prevented = false;
    if (!DispatchFormEvent(realm, context, WrapperKey{document, node},
                           "invalid", false, nullptr, &prevented, error))
      return false;
  }
  return true;
}

bool ReadOptionalSubmitter(v8::Isolate *isolate,
                           const v8::FunctionCallbackInfo<v8::Value> &info,
                           WrapperKey *submitter) {
  *submitter = WrapperKey{};
  if (info.Length() == 0 || info[0]->IsUndefined() || info[0]->IsNull())
    return true;
  if (!info[0]->IsObject()) {
    ThrowTypeError(isolate, "form submitter must be an element");
    return false;
  }
  return ReadReceiverKey(isolate, info[0].As<v8::Object>(), submitter);
}

bool SubmitHostForm(gossamer_v8_realm *realm, const WrapperKey &form,
                    const WrapperKey &submitter, std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->submit_form(
          realm->active_host->execution_id, form.document, form.node,
          submitter.document, submitter.node, &host_error) == 0) {
    *error = TakeCString(host_error);
    return false;
  }
  std::free(host_error);
  return true;
}

bool MarkHostFormUserValidityForSubmission(gossamer_v8_realm *realm,
                                           const WrapperKey &form,
                                           std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->mark_form_user_validity_for_submission(
          realm->active_host->execution_id, form.document, form.node,
          &host_error) == 0) {
    *error = TakeCString(host_error);
    return false;
  }
  std::free(host_error);
  return true;
}

void HTMLFormElementCheckValidity(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey form;
  if (!ReadReceiverKey(isolate, info.This(), &form))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  bool valid = false;
  std::vector<uint32_t> invalid;
  std::string error;
  if (!ReadHostFormValidity(realm, form, &valid, &invalid, &error) ||
      (!valid && !DispatchInvalidControls(realm, isolate->GetCurrentContext(),
                                          form.document, invalid, &error))) {
    ThrowError(isolate,
               error.empty() ? "checking form validity failed" : error);
    return;
  }
  info.GetReturnValue().Set(valid);
}

void HTMLFormElementSubmit(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey form;
  if (!ReadReceiverKey(isolate, info.This(), &form))
    return;
  std::string error;
  if (!SubmitHostForm(CurrentRealm(isolate), form, WrapperKey{}, &error))
    ThrowError(isolate, error.empty() ? "submitting form failed" : error);
}

void HTMLFormElementRequestSubmit(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  WrapperKey form;
  WrapperKey submitter;
  if (!ReadReceiverKey(isolate, info.This(), &form) ||
      !ReadOptionalSubmitter(isolate, info, &submitter))
    return;
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  bool skip_validation = false;
  bool found = false;
  std::string ignored;
  if (!ReadAttribute(realm, form, "novalidate", &ignored, &found, &error)) {
    ThrowError(isolate, error);
    return;
  }
  skip_validation = found;
  if (!skip_validation && submitter.node != 0) {
    if (!ReadAttribute(realm, submitter, "formnovalidate", &ignored, &found,
                       &error)) {
      ThrowError(isolate, error);
      return;
    }
    skip_validation = found;
  }
  if (!MarkHostFormUserValidityForSubmission(realm, form, &error)) {
    ThrowError(isolate,
               error.empty() ? "marking form user validity failed" : error);
    return;
  }
  if (!skip_validation) {
    bool valid = false;
    std::vector<uint32_t> invalid;
    if (!ReadHostFormValidity(realm, form, &valid, &invalid, &error) ||
        (!valid &&
         !DispatchInvalidControls(realm, isolate->GetCurrentContext(),
                                  form.document, invalid, &error))) {
      ThrowError(isolate,
                 error.empty() ? "checking form validity failed" : error);
      return;
    }
    if (!valid)
      return;
  }
  bool prevented = false;
  const WrapperKey *event_submitter = submitter.node == 0 ? nullptr : &submitter;
  bool dispatched = DispatchFormEvent(
      realm, isolate->GetCurrentContext(), form, "submit", true,
      event_submitter, &prevented, &error);
  bool submitted = false;
  if (dispatched && !prevented)
    submitted = SubmitHostForm(realm, form, submitter, &error);
  if (!dispatched || (!prevented && !submitted)) {
    ThrowError(isolate, error.empty() ? "requesting form submission failed"
                                     : error);
    return;
  }
}

v8::Local<v8::Array> ReadFormDataEntries(v8::Isolate *isolate,
                                         v8::Local<v8::Object> receiver) {
  if (receiver.IsEmpty() || receiver->InternalFieldCount() != 1) {
    ThrowTypeError(isolate, "FormData receiver is invalid");
    return {};
  }
  v8::Local<v8::Data> data =
      receiver->GetInternalField(kFormDataEntriesField);
  if (!data->IsValue() || !data.As<v8::Value>()->IsArray()) {
    ThrowTypeError(isolate, "FormData entries are unavailable");
    return {};
  }
  return data.As<v8::Value>().As<v8::Array>();
}

bool ReadFormDataPair(v8::Local<v8::Context> context,
                      v8::Local<v8::Array> entries, uint32_t index,
                      v8::Local<v8::Array> *pair) {
  v8::Local<v8::Value> value;
  if (!entries->Get(context, index).ToLocal(&value) || !value->IsArray())
    return false;
  *pair = value.As<v8::Array>();
  return true;
}

bool FormDataStringArgument(v8::Isolate *isolate,
                            const v8::FunctionCallbackInfo<v8::Value> &info,
                            int index, const char *label,
                            v8::Local<v8::String> *output) {
  if (info.Length() <= index) {
    ThrowTypeError(isolate, label);
    return false;
  }
  return info[index]->ToString(isolate->GetCurrentContext()).ToLocal(output);
}

void FormDataConstructor(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  if (!info.IsConstructCall()) {
    ThrowTypeError(isolate, "FormData constructor must be called with new");
    return;
  }
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Array> entries = v8::Array::New(isolate);
  if (info.Length() > 0 && !info[0]->IsUndefined() && !info[0]->IsNull()) {
    if (!info[0]->IsObject()) {
      ThrowTypeError(isolate, "FormData form must be an element");
      return;
    }
    WrapperKey form;
    if (!ReadReceiverKey(isolate, info[0].As<v8::Object>(), &form))
      return;
    WrapperKey submitter;
    if (info.Length() > 1 && !info[1]->IsUndefined() && !info[1]->IsNull()) {
      if (!info[1]->IsObject() ||
          !ReadReceiverKey(isolate, info[1].As<v8::Object>(), &submitter))
        return;
    }
    gossamer_v8_realm *realm = CurrentRealm(isolate);
    std::string error;
    if (!RequireHost(realm, &error)) {
      ThrowError(isolate, error);
      return;
    }
    char *json_data = nullptr;
    size_t json_length = 0;
    char *host_error = nullptr;
    if (realm->active_host->form_data_json(
            realm->active_host->execution_id, form.document, form.node,
            submitter.document, submitter.node, &json_data, &json_length,
            &host_error) == 0) {
      error = TakeCString(host_error);
      std::free(json_data);
      ThrowError(isolate, error.empty() ? "constructing FormData failed"
                                       : error);
      return;
    }
    std::free(host_error);
    v8::Local<v8::String> json;
    if (!NewUTF8String(isolate, json_data, json_length, &json)) {
      std::free(json_data);
      ThrowError(isolate, "V8 failed to decode FormData entries");
      return;
    }
    std::free(json_data);
    v8::Local<v8::Value> parsed;
    if (!v8::JSON::Parse(context, json).ToLocal(&parsed) ||
        !parsed->IsArray()) {
      ThrowError(isolate, "V8 failed to parse FormData entries");
      return;
    }
    v8::Local<v8::Array> objects = parsed.As<v8::Array>();
    entries = v8::Array::New(isolate, objects->Length());
    for (uint32_t index = 0; index < objects->Length(); ++index) {
      v8::Local<v8::Value> value;
      if (!objects->Get(context, index).ToLocal(&value) ||
          !value->IsObject()) {
        ThrowError(isolate, "FormData host returned an invalid entry");
        return;
      }
      v8::Local<v8::Object> object = value.As<v8::Object>();
      v8::Local<v8::Value> name;
      v8::Local<v8::Value> entry_value;
      v8::Local<v8::Array> pair = v8::Array::New(isolate, 2);
      if (!object
               ->Get(context,
                     v8::String::NewFromUtf8Literal(isolate, "name"))
               .ToLocal(&name) ||
          !object
               ->Get(context,
                     v8::String::NewFromUtf8Literal(isolate, "value"))
               .ToLocal(&entry_value) ||
          !pair->Set(context, 0, name).FromMaybe(false) ||
          !pair->Set(context, 1, entry_value).FromMaybe(false) ||
          !entries->Set(context, index, pair).FromMaybe(false)) {
        ThrowError(isolate, "V8 failed to build FormData entries");
        return;
      }
    }
  }
  info.This()->SetInternalField(kFormDataEntriesField, entries);
  info.GetReturnValue().Set(info.This());
}

void FormDataAppend(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Array> entries = ReadFormDataEntries(isolate, info.This());
  if (entries.IsEmpty())
    return;
  v8::Local<v8::String> name;
  v8::Local<v8::String> value;
  if (!FormDataStringArgument(isolate, info, 0,
                              "FormData.append requires a name", &name) ||
      !FormDataStringArgument(isolate, info, 1,
                              "FormData.append requires a value", &value))
    return;
  v8::Local<v8::Array> pair = v8::Array::New(isolate, 2);
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  if (!pair->Set(context, 0, name).FromMaybe(false) ||
      !pair->Set(context, 1, value).FromMaybe(false) ||
      !entries->Set(context, entries->Length(), pair).FromMaybe(false))
    ThrowError(isolate, "V8 failed to append FormData");
}

void FormDataGet(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Array> entries = ReadFormDataEntries(isolate, info.This());
  v8::Local<v8::String> name;
  if (entries.IsEmpty() ||
      !FormDataStringArgument(isolate, info, 0,
                              "FormData.get requires a name", &name))
    return;
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  for (uint32_t index = 0; index < entries->Length(); ++index) {
    v8::Local<v8::Array> pair;
    v8::Local<v8::Value> pair_name;
    v8::Local<v8::Value> pair_value;
    if (!ReadFormDataPair(context, entries, index, &pair) ||
        !pair->Get(context, 0).ToLocal(&pair_name) ||
        !pair->Get(context, 1).ToLocal(&pair_value))
      continue;
    if (pair_name->StrictEquals(name)) {
      info.GetReturnValue().Set(pair_value);
      return;
    }
  }
  info.GetReturnValue().Set(v8::Null(isolate));
}

void FormDataGetAll(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Array> entries = ReadFormDataEntries(isolate, info.This());
  v8::Local<v8::String> name;
  if (entries.IsEmpty() ||
      !FormDataStringArgument(isolate, info, 0,
                              "FormData.getAll requires a name", &name))
    return;
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Array> result = v8::Array::New(isolate);
  for (uint32_t index = 0; index < entries->Length(); ++index) {
    v8::Local<v8::Array> pair;
    v8::Local<v8::Value> pair_name;
    v8::Local<v8::Value> pair_value;
    if (ReadFormDataPair(context, entries, index, &pair) &&
        pair->Get(context, 0).ToLocal(&pair_name) &&
        pair->Get(context, 1).ToLocal(&pair_value) &&
        pair_name->StrictEquals(name) &&
        !result->Set(context, result->Length(), pair_value).FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to collect FormData values");
      return;
    }
  }
  info.GetReturnValue().Set(result);
}

void FormDataHas(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Array> entries = ReadFormDataEntries(isolate, info.This());
  v8::Local<v8::String> name;
  if (entries.IsEmpty() ||
      !FormDataStringArgument(isolate, info, 0,
                              "FormData.has requires a name", &name))
    return;
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  for (uint32_t index = 0; index < entries->Length(); ++index) {
    v8::Local<v8::Array> pair;
    v8::Local<v8::Value> pair_name;
    if (ReadFormDataPair(context, entries, index, &pair) &&
        pair->Get(context, 0).ToLocal(&pair_name) &&
        pair_name->StrictEquals(name)) {
      info.GetReturnValue().Set(true);
      return;
    }
  }
  info.GetReturnValue().Set(false);
}

void FormDataDelete(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Array> entries = ReadFormDataEntries(isolate, info.This());
  v8::Local<v8::String> name;
  if (entries.IsEmpty() ||
      !FormDataStringArgument(isolate, info, 0,
                              "FormData.delete requires a name", &name))
    return;
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Array> retained = v8::Array::New(isolate);
  for (uint32_t index = 0; index < entries->Length(); ++index) {
    v8::Local<v8::Array> pair;
    v8::Local<v8::Value> pair_name;
    if (!ReadFormDataPair(context, entries, index, &pair) ||
        !pair->Get(context, 0).ToLocal(&pair_name))
      continue;
    if (!pair_name->StrictEquals(name) &&
        !retained->Set(context, retained->Length(), pair).FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to delete FormData entries");
      return;
    }
  }
  info.This()->SetInternalField(kFormDataEntriesField, retained);
}

void FormDataSet(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Array> entries = ReadFormDataEntries(isolate, info.This());
  v8::Local<v8::String> name;
  v8::Local<v8::String> value;
  if (entries.IsEmpty() ||
      !FormDataStringArgument(isolate, info, 0,
                              "FormData.set requires a name", &name) ||
      !FormDataStringArgument(isolate, info, 1,
                              "FormData.set requires a value", &value))
    return;
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Array> updated = v8::Array::New(isolate);
  bool replaced = false;
  for (uint32_t index = 0; index < entries->Length(); ++index) {
    v8::Local<v8::Array> pair;
    v8::Local<v8::Value> pair_name;
    if (!ReadFormDataPair(context, entries, index, &pair) ||
        !pair->Get(context, 0).ToLocal(&pair_name))
      continue;
    if (pair_name->StrictEquals(name)) {
      if (replaced)
        continue;
      pair = v8::Array::New(isolate, 2);
      if (!pair->Set(context, 0, name).FromMaybe(false) ||
          !pair->Set(context, 1, value).FromMaybe(false)) {
        ThrowError(isolate, "V8 failed to set FormData entry");
        return;
      }
      replaced = true;
    }
    if (!updated->Set(context, updated->Length(), pair).FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to set FormData entry");
      return;
    }
  }
  if (!replaced) {
    v8::Local<v8::Array> pair = v8::Array::New(isolate, 2);
    if (!pair->Set(context, 0, name).FromMaybe(false) ||
        !pair->Set(context, 1, value).FromMaybe(false) ||
        !updated->Set(context, updated->Length(), pair).FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to set FormData entry");
      return;
    }
  }
  info.This()->SetInternalField(kFormDataEntriesField, updated);
}

void FormDataForEach(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Array> entries = ReadFormDataEntries(isolate, info.This());
  if (entries.IsEmpty())
    return;
  if (info.Length() == 0 || !info[0]->IsFunction()) {
    ThrowTypeError(isolate, "FormData.forEach requires a callback");
    return;
  }
  v8::Local<v8::Function> callback = info[0].As<v8::Function>();
  v8::Local<v8::Value> receiver =
      info.Length() > 1 ? info[1] : v8::Undefined(isolate);
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  for (uint32_t index = 0; index < entries->Length(); ++index) {
    v8::Local<v8::Array> pair;
    v8::Local<v8::Value> name;
    v8::Local<v8::Value> value;
    if (!ReadFormDataPair(context, entries, index, &pair) ||
        !pair->Get(context, 0).ToLocal(&name) ||
        !pair->Get(context, 1).ToLocal(&value))
      continue;
    v8::Local<v8::Value> arguments[] = {value, name, info.This()};
    v8::Local<v8::Value> ignored;
    if (!callback->Call(context, receiver, 3, arguments).ToLocal(&ignored))
      return;
  }
}

bool FormDataArrayIterator(v8::Isolate *isolate,
                           v8::Local<v8::Array> values,
                           const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Value> method;
  if (!values
           ->Get(context,
                 v8::String::NewFromUtf8Literal(isolate, "values"))
           .ToLocal(&method) ||
      !method->IsFunction()) {
    ThrowError(isolate, "V8 Array iterator is unavailable");
    return false;
  }
  v8::Local<v8::Value> iterator;
  if (!method.As<v8::Function>()->Call(context, values, 0, nullptr)
           .ToLocal(&iterator))
    return false;
  info.GetReturnValue().Set(iterator);
  return true;
}

void FormDataEntries(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Local<v8::Array> entries = ReadFormDataEntries(info.GetIsolate(), info.This());
  if (!entries.IsEmpty())
    FormDataArrayIterator(info.GetIsolate(), entries, info);
}

void FormDataKeysOrValues(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Array> entries = ReadFormDataEntries(isolate, info.This());
  if (entries.IsEmpty())
    return;
  bool keys = info.Data().As<v8::Boolean>()->Value();
  v8::Local<v8::Array> values = v8::Array::New(isolate, entries->Length());
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  for (uint32_t index = 0; index < entries->Length(); ++index) {
    v8::Local<v8::Array> pair;
    v8::Local<v8::Value> value;
    if (!ReadFormDataPair(context, entries, index, &pair) ||
        !pair->Get(context, keys ? 0 : 1).ToLocal(&value) ||
        !values->Set(context, index, value).FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to iterate FormData");
      return;
    }
  }
  FormDataArrayIterator(isolate, values, info);
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

void FetchHostCallback(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error) || realm->active_host->fetch == nullptr) {
    ThrowError(isolate, error.empty() ? "fetch is unavailable" : error);
    return;
  }
  v8::Local<v8::String> request_value;
  if (info.Length() < 1 ||
      !info[0]->ToString(isolate->GetCurrentContext()).ToLocal(&request_value))
    return;
  std::string request = UTF8Value(isolate, request_value);
  char *response = nullptr;
  size_t response_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->fetch(
          realm->active_host->execution_id, request.data(), request.size(),
          &response, &response_length, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(response);
    ThrowError(isolate, error.empty() ? "fetch failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> result;
  if (!v8::String::NewFromUtf8(isolate, response, v8::NewStringType::kNormal,
                               static_cast<int>(response_length))
           .ToLocal(&result)) {
    std::free(response);
    return;
  }
  std::free(response);
  info.GetReturnValue().Set(result);
}

void StorageHostCallback(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error) || realm->active_host->storage == nullptr) {
    ThrowError(isolate, error.empty() ? "storage is unavailable" : error);
    return;
  }
  v8::Local<v8::String> request_value;
  if (info.Length() < 1 ||
      !info[0]->ToString(isolate->GetCurrentContext()).ToLocal(&request_value))
    return;
  std::string request = UTF8Value(isolate, request_value);
  char *response = nullptr;
  size_t response_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->storage(
          realm->active_host->execution_id, request.data(), request.size(),
          &response, &response_length, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(response);
    ThrowError(isolate, error.empty() ? "storage operation failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> result;
  if (!v8::String::NewFromUtf8(isolate, response, v8::NewStringType::kNormal,
                               static_cast<int>(response_length))
           .ToLocal(&result)) {
    std::free(response);
    return;
  }
  std::free(response);
  info.GetReturnValue().Set(result);
}

void WebSocketHostCallback(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error) || realm->active_host->websocket == nullptr) {
    ThrowError(isolate, error.empty() ? "WebSocket is unavailable" : error);
    return;
  }
  v8::Local<v8::String> request_value;
  if (info.Length() < 1 ||
      !info[0]->ToString(isolate->GetCurrentContext()).ToLocal(&request_value))
    return;
  std::string request = UTF8Value(isolate, request_value);
  char *response = nullptr;
  size_t response_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->websocket(
          realm->active_host->execution_id, request.data(), request.size(),
          &response, &response_length, &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(response);
    ThrowError(isolate, error.empty() ? "WebSocket operation failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> result;
  if (!v8::String::NewFromUtf8(isolate, response, v8::NewStringType::kNormal,
                               static_cast<int>(response_length))
           .ToLocal(&result)) {
    std::free(response);
    return;
  }
  std::free(response);
  info.GetReturnValue().Set(result);
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

void RequestAnimationFrameCallback(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (info.Length() == 0 || !info[0]->IsFunction()) {
    ThrowTypeError(isolate, "requestAnimationFrame requires a function");
    return;
  }
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  uint64_t callback = StoreOneShotCallback(realm, info[0].As<v8::Function>());
  uint64_t frame = 0;
  char *host_error = nullptr;
  if (realm->active_host->request_animation_frame(
          realm->active_host->execution_id, callback, &frame,
          &host_error) == 0) {
    error = TakeCString(host_error);
    RemoveCallback(realm, callback);
    ThrowError(isolate,
               error.empty() ? "requestAnimationFrame failed" : error);
    return;
  }
  std::free(host_error);
  realm->animation_frame_callbacks[frame] = callback;
  realm->callback_animation_frames[callback] = frame;
  info.GetReturnValue().Set(
      v8::Number::New(isolate, static_cast<double>(frame)));
}

void CancelAnimationFrameCallback(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  if (info.Length() == 0)
    return;
  uint64_t frame = 0;
  if (!TimerIDFromValue(isolate->GetCurrentContext(), info[0], &frame))
    return;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->cancel_animation_frame(
          realm->active_host->execution_id, frame, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate,
               error.empty() ? "cancelAnimationFrame failed" : error);
    return;
  }
  std::free(host_error);
  auto callback = realm->animation_frame_callbacks.find(frame);
  if (callback != realm->animation_frame_callbacks.end())
    RemoveCallback(realm, callback->second);
}

void PerformanceNowCallback(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  double milliseconds = 0;
  char *host_error = nullptr;
  if (realm->active_host->performance_now(
          realm->active_host->execution_id, &milliseconds,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "performance.now failed" : error);
    return;
  }
  std::free(host_error);
  info.GetReturnValue().Set(v8::Number::New(isolate, milliseconds));
}

bool ReadElementGeometry(gossamer_v8_realm *realm, const WrapperKey &key,
                         gossamer_v8_element_geometry *geometry,
                         std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->element_geometry(
          realm->active_host->execution_id, key.document, key.node, geometry,
          &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "reading element geometry failed";
    return false;
  }
  std::free(host_error);
  return true;
}

bool ReadElementClientRectCount(gossamer_v8_realm *realm,
                                const WrapperKey &key, size_t *count,
                                std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->element_client_rect_count(
          realm->active_host->execution_id, key.document, key.node, count,
          &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "reading element client rect count failed";
    return false;
  }
  std::free(host_error);
  return true;
}

bool ReadElementClientRect(gossamer_v8_realm *realm, const WrapperKey &key,
                           size_t index, gossamer_v8_rect *rect, bool *found,
                           std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  int host_found = 0;
  char *host_error = nullptr;
  if (realm->active_host->element_client_rect(
          realm->active_host->execution_id, key.document, key.node, index,
          rect, &host_found, &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "reading element client rect failed";
    return false;
  }
  std::free(host_error);
  *found = host_found != 0;
  return true;
}

bool ReadViewportGeometry(gossamer_v8_realm *realm,
                          gossamer_v8_viewport_geometry *geometry,
                          std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->viewport_geometry(
          realm->active_host->execution_id, geometry, &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "reading viewport geometry failed";
    return false;
  }
  std::free(host_error);
  return true;
}

bool DefineNumber(v8::Local<v8::Context> context,
                  v8::Local<v8::Object> object, const char *name,
                  double value, bool read_only = true) {
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  v8::PropertyAttribute attributes = v8::DontEnum;
  if (read_only)
    attributes = static_cast<v8::PropertyAttribute>(attributes | v8::ReadOnly);
  return object
      ->DefineOwnProperty(
          context,
          v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
          v8::Number::New(isolate, value), attributes)
      .FromMaybe(false);
}

bool InitializeDOMRect(v8::Local<v8::Context> context,
                       v8::Local<v8::Object> object, double x, double y,
                       double width, double height) {
  double left = std::min(x, x + width);
  double right = std::max(x, x + width);
  double top = std::min(y, y + height);
  double bottom = std::max(y, y + height);
  return DefineNumber(context, object, "x", x) &&
         DefineNumber(context, object, "y", y) &&
         DefineNumber(context, object, "width", width) &&
         DefineNumber(context, object, "height", height) &&
         DefineNumber(context, object, "top", top) &&
         DefineNumber(context, object, "right", right) &&
         DefineNumber(context, object, "bottom", bottom) &&
         DefineNumber(context, object, "left", left);
}

void DOMRectConstructor(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  if (!info.IsConstructCall()) {
    ThrowTypeError(isolate, "DOMRect constructor requires new");
    return;
  }
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  double values[4] = {0, 0, 0, 0};
  for (int index = 0; index < info.Length() && index < 4; ++index) {
    if (!info[index]->NumberValue(context).To(&values[index]))
      return;
  }
  if (!InitializeDOMRect(context, info.This(), values[0], values[1], values[2],
                         values[3])) {
    ThrowError(isolate, "V8 failed to initialize DOMRect");
  }
}

void DOMRectToJSON(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Object> result = v8::Object::New(isolate);
  for (const char *name : {"x", "y", "width", "height", "top", "right",
                           "bottom", "left"}) {
    v8::Local<v8::String> property =
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked();
    v8::Local<v8::Value> value;
    if (!info.This()->Get(context, property).ToLocal(&value) ||
        !result->Set(context, property, value).FromMaybe(false))
      return;
  }
  info.GetReturnValue().Set(result);
}

v8::MaybeLocal<v8::Object> CreateDOMRect(gossamer_v8_realm *realm,
                                         v8::Local<v8::Context> context,
                                         const gossamer_v8_rect &rect) {
  v8::Local<v8::Object> object;
  if (!realm->dom_rect_template.Get(realm->isolate)
           ->InstanceTemplate()
           ->NewInstance(context)
           .ToLocal(&object) ||
      !InitializeDOMRect(context, object, rect.x, rect.y, rect.width,
                         rect.height)) {
    return {};
  }
  return object;
}

LayoutObserverState *ReadLayoutObserverState(
    v8::Isolate *isolate, v8::Local<v8::Object> receiver) {
  if (receiver.IsEmpty() ||
      receiver->InternalFieldCount() <= kLayoutObserverStateField) {
    ThrowTypeError(isolate, "layout observer receiver is invalid");
    return nullptr;
  }
  v8::Local<v8::Data> data =
      receiver->GetInternalField(kLayoutObserverStateField);
  if (!data->IsValue() || !data.As<v8::Value>()->IsExternal()) {
    ThrowTypeError(isolate, "layout observer lost its native state");
    return nullptr;
  }
  return static_cast<LayoutObserverState *>(
      data.As<v8::Value>().As<v8::External>()->Value(
          v8::kExternalPointerTypeTagDefault));
}

void LayoutObserverCollected(
    const v8::WeakCallbackInfo<LayoutObserverWeakData> &info) {
  LayoutObserverWeakData *weak = info.GetParameter();
  if (weak == nullptr)
    return;
  if (weak->realm != nullptr)
    weak->realm->layout_observers.erase(weak);
  weak->object.Reset();
  if (weak->state != nullptr) {
    weak->state->callback.Reset();
    weak->state->registrations.clear();
    delete weak->state;
  }
  delete weak;
}

bool ParseIntersectionThresholds(v8::Local<v8::Context> context,
                                 v8::Local<v8::Value> options,
                                 std::vector<double> *thresholds) {
  if (options->IsUndefined())
    return true;
  if (!options->IsObject())
    return false;
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  v8::Local<v8::Value> value;
  if (!options.As<v8::Object>()
           ->Get(context,
                 v8::String::NewFromUtf8Literal(isolate, "threshold"))
           .ToLocal(&value))
    return false;
  if (value->IsUndefined())
    return true;
  thresholds->clear();
  auto append = [context, thresholds](v8::Local<v8::Value> item) {
    double threshold = 0;
    if (!item->NumberValue(context).To(&threshold) ||
        !std::isfinite(threshold) || threshold < 0 || threshold > 1)
      return false;
    thresholds->push_back(threshold);
    return true;
  };
  if (value->IsArray()) {
    v8::Local<v8::Array> values = value.As<v8::Array>();
    for (uint32_t index = 0; index < values->Length(); ++index) {
      v8::Local<v8::Value> item;
      if (!values->Get(context, index).ToLocal(&item) || !append(item))
        return false;
    }
  } else if (!append(value)) {
    return false;
  }
  if (thresholds->empty())
    thresholds->push_back(0);
  std::sort(thresholds->begin(), thresholds->end());
  thresholds->erase(std::unique(thresholds->begin(), thresholds->end()),
                    thresholds->end());
  return true;
}

void LayoutObserverConstructor(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  if (!info.IsConstructCall() || info.Length() == 0 ||
      !info[0]->IsFunction()) {
    ThrowTypeError(isolate, "observer requires a callback");
    return;
  }
  auto state = std::make_unique<LayoutObserverState>();
  state->realm = CurrentRealm(isolate);
  state->kind = info.Data()->IsTrue()
                    ? LayoutObserverKind::Intersection
                    : LayoutObserverKind::Resize;
  state->callback.Reset(isolate, info[0].As<v8::Function>());
  if (state->kind == LayoutObserverKind::Intersection &&
      !ParseIntersectionThresholds(
          context,
          info.Length() > 1 ? info[1] : v8::Undefined(isolate),
          &state->thresholds)) {
    ThrowTypeError(isolate, "IntersectionObserver threshold is invalid");
    return;
  }
  auto weak = std::make_unique<LayoutObserverWeakData>();
  weak->realm = state->realm;
  weak->state = state.get();
  weak->object.Reset(isolate, info.This());
  state->weak = weak.get();
  info.This()->SetInternalField(
      kLayoutObserverStateField,
      v8::External::New(isolate, state.get(),
                        v8::kExternalPointerTypeTagDefault));
  if (state->kind == LayoutObserverKind::Intersection) {
    v8::Local<v8::Array> thresholds =
        v8::Array::New(isolate, static_cast<int>(state->thresholds.size()));
    for (uint32_t index = 0; index < state->thresholds.size(); ++index) {
      if (!thresholds
               ->Set(context, index,
                     v8::Number::New(isolate, state->thresholds[index]))
               .FromMaybe(false)) {
        ThrowError(isolate, "V8 failed to expose observer thresholds");
        return;
      }
    }
    constexpr auto read_only =
        static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontEnum);
    if (!info.This()
             ->DefineOwnProperty(
                 context, v8::String::NewFromUtf8Literal(isolate, "root"),
                 v8::Null(isolate), read_only)
             .FromMaybe(false) ||
        !info.This()
             ->DefineOwnProperty(
                 context,
                 v8::String::NewFromUtf8Literal(isolate, "rootMargin"),
                 v8::String::NewFromUtf8Literal(isolate, "0px 0px 0px 0px"),
                 read_only)
             .FromMaybe(false) ||
        !info.This()
             ->DefineOwnProperty(
                 context,
                 v8::String::NewFromUtf8Literal(isolate, "thresholds"),
                 thresholds, read_only)
             .FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to expose observer options");
      return;
    }
  }
  weak->object.SetWeak(weak.get(), LayoutObserverCollected,
                       v8::WeakCallbackType::kParameter);
  state->realm->layout_observers.insert(weak.get());
  state.release();
  weak.release();
  info.GetReturnValue().Set(info.This());
}

void LayoutObserverObserve(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  LayoutObserverState *state =
      ReadLayoutObserverState(isolate, info.This());
  WrapperKey target;
  if (state == nullptr || info.Length() == 0 || !info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &target)) {
    if (state != nullptr)
      ThrowTypeError(isolate, "observe requires a native Element target");
    return;
  }
  auto existing = std::find_if(
      state->registrations.begin(), state->registrations.end(),
      [&target](const LayoutObserverRegistration &registration) {
        return registration.target == target;
      });
  if (existing == state->registrations.end()) {
    LayoutObserverRegistration registration;
    registration.target = target;
    registration.target_object.Reset(isolate, info[0].As<v8::Object>());
    state->registrations.push_back(std::move(registration));
  } else {
    existing->target_object.Reset(isolate, info[0].As<v8::Object>());
  }
}

void LayoutObserverUnobserve(const v8::FunctionCallbackInfo<v8::Value> &info) {
  LayoutObserverState *state =
      ReadLayoutObserverState(info.GetIsolate(), info.This());
  WrapperKey target;
  if (state == nullptr || info.Length() == 0 || !info[0]->IsObject() ||
      !ReadWrapperKey(info[0].As<v8::Object>(), &target))
    return;
  state->registrations.erase(
      std::remove_if(
          state->registrations.begin(), state->registrations.end(),
          [&target](const LayoutObserverRegistration &registration) {
            return registration.target == target;
          }),
      state->registrations.end());
}

void LayoutObserverDisconnect(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  LayoutObserverState *state =
      ReadLayoutObserverState(info.GetIsolate(), info.This());
  if (state != nullptr)
    state->registrations.clear();
}

void LayoutObserverTakeRecords(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  info.GetReturnValue().Set(v8::Array::New(info.GetIsolate()));
}

bool SetObserverEntryProperty(v8::Local<v8::Context> context,
                              v8::Local<v8::Object> object,
                              const char *name,
                              v8::Local<v8::Value> value) {
  return object
      ->DefineOwnProperty(
          context,
          v8::String::NewFromUtf8(v8::Isolate::GetCurrent(), name)
              .ToLocalChecked(),
          value,
          static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontEnum))
      .FromMaybe(false);
}

v8::MaybeLocal<v8::Object> BuildResizeObserverEntry(
    LayoutObserverState *state, v8::Local<v8::Context> context,
    LayoutObserverRegistration &registration,
    const gossamer_v8_element_geometry &geometry) {
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  v8::Local<v8::Object> entry = v8::Object::New(isolate);
  v8::Local<v8::Object> target = registration.target_object.Get(isolate);
  gossamer_v8_rect content{0, 0, geometry.client_width,
                           geometry.client_height};
  v8::Local<v8::Object> content_rect;
  if (!CreateDOMRect(state->realm, context, content).ToLocal(&content_rect))
    return {};
  v8::Local<v8::Object> size = v8::Object::New(isolate);
  if (!DefineNumber(context, size, "inlineSize", geometry.client_width) ||
      !DefineNumber(context, size, "blockSize", geometry.client_height))
    return {};
  v8::Local<v8::Array> sizes = v8::Array::New(isolate, 1);
  if (!sizes->Set(context, 0, size).FromMaybe(false) ||
      !SetObserverEntryProperty(context, entry, "target", target) ||
      !SetObserverEntryProperty(context, entry, "contentRect", content_rect) ||
      !SetObserverEntryProperty(context, entry, "contentBoxSize", sizes) ||
      !SetObserverEntryProperty(context, entry, "borderBoxSize", sizes) ||
      !SetObserverEntryProperty(context, entry, "devicePixelContentBoxSize",
                                sizes))
    return {};
  return entry;
}

gossamer_v8_rect IntersectRects(const gossamer_v8_rect &left,
                                const gossamer_v8_rect &right) {
  double x = std::max(left.x, right.x);
  double y = std::max(left.y, right.y);
  double end_x = std::min(left.x + left.width, right.x + right.width);
  double end_y = std::min(left.y + left.height, right.y + right.height);
  return gossamer_v8_rect{x, y, std::max(0.0, end_x - x),
                          std::max(0.0, end_y - y)};
}

bool IntersectionThresholdCrossed(const LayoutObserverState *state,
                                  double before, double after) {
  for (double threshold : state->thresholds) {
    if ((before < threshold && after >= threshold) ||
        (before >= threshold && after < threshold))
      return true;
  }
  return false;
}

v8::MaybeLocal<v8::Object> BuildIntersectionObserverEntry(
    LayoutObserverState *state, v8::Local<v8::Context> context,
    LayoutObserverRegistration &registration,
    const gossamer_v8_element_geometry &geometry,
    const gossamer_v8_rect &root, const gossamer_v8_rect &intersection,
    double ratio, bool intersecting, double timestamp) {
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  v8::Local<v8::Object> entry = v8::Object::New(isolate);
  v8::Local<v8::Object> target = registration.target_object.Get(isolate);
  v8::Local<v8::Object> bounds;
  v8::Local<v8::Object> root_bounds;
  v8::Local<v8::Object> intersection_rect;
  if (!CreateDOMRect(state->realm, context, geometry.rect).ToLocal(&bounds) ||
      !CreateDOMRect(state->realm, context, root).ToLocal(&root_bounds) ||
      !CreateDOMRect(state->realm, context, intersection)
           .ToLocal(&intersection_rect) ||
      !SetObserverEntryProperty(context, entry, "target", target) ||
      !SetObserverEntryProperty(context, entry, "boundingClientRect", bounds) ||
      !SetObserverEntryProperty(context, entry, "rootBounds", root_bounds) ||
      !SetObserverEntryProperty(context, entry, "intersectionRect",
                                intersection_rect) ||
      !SetObserverEntryProperty(context, entry, "isIntersecting",
                                v8::Boolean::New(isolate, intersecting)) ||
      !SetObserverEntryProperty(context, entry, "intersectionRatio",
                                v8::Number::New(isolate, ratio)) ||
      !SetObserverEntryProperty(context, entry, "time",
                                v8::Number::New(isolate, timestamp)))
    return {};
  return entry;
}

bool DeliverLayoutObservers(gossamer_v8_realm *realm,
                            v8::Local<v8::Context> context,
                            std::string *error) {
  constexpr size_t kMaximumRounds = 10;
  for (size_t round = 0; round < kMaximumRounds; ++round) {
    bool delivered = false;
    std::vector<LayoutObserverWeakData *> observers(
        realm->layout_observers.begin(), realm->layout_observers.end());
    for (LayoutObserverWeakData *weak : observers) {
      if (weak == nullptr || weak->state == nullptr)
        continue;
      LayoutObserverState *state = weak->state;
      v8::Local<v8::Array> entries = v8::Array::New(realm->isolate);
      gossamer_v8_viewport_geometry viewport{};
      if (state->kind == LayoutObserverKind::Intersection &&
          !ReadViewportGeometry(realm, &viewport, error))
        return false;
      double timestamp = 0;
      if (state->kind == LayoutObserverKind::Intersection) {
        char *host_error = nullptr;
        if (realm->active_host->performance_now(
                realm->active_host->execution_id, &timestamp,
                &host_error) == 0) {
          *error = TakeCString(host_error);
          return false;
        }
        std::free(host_error);
      }
      for (auto &registration : state->registrations) {
        gossamer_v8_element_geometry geometry{};
        if (!ReadElementGeometry(realm, registration.target, &geometry,
                                 error))
          return false;
        v8::Local<v8::Object> entry;
        bool changed = false;
        if (state->kind == LayoutObserverKind::Resize) {
          changed = !registration.has_last ||
                    registration.last_width != geometry.client_width ||
                    registration.last_height != geometry.client_height;
          if (changed &&
              !BuildResizeObserverEntry(state, context, registration, geometry)
                   .ToLocal(&entry)) {
            *error = "V8 failed to build ResizeObserverEntry";
            return false;
          }
          registration.last_width = geometry.client_width;
          registration.last_height = geometry.client_height;
        } else {
          gossamer_v8_rect root{0, 0, viewport.inner_width,
                                viewport.inner_height};
          gossamer_v8_rect intersection =
              IntersectRects(root, geometry.rect);
          double target_area = geometry.rect.width * geometry.rect.height;
          double intersection_area = intersection.width * intersection.height;
          bool intersecting = intersection.width > 0 && intersection.height > 0;
          double ratio = target_area > 0 ? intersection_area / target_area : 0;
          changed = !registration.has_last ||
                    registration.last_intersecting != intersecting ||
                    IntersectionThresholdCrossed(state,
                                                 registration.last_ratio,
                                                 ratio);
          if (changed && !BuildIntersectionObserverEntry(
                              state, context, registration, geometry, root,
                              intersection, ratio, intersecting, timestamp)
                              .ToLocal(&entry)) {
            *error = "V8 failed to build IntersectionObserverEntry";
            return false;
          }
          registration.last_ratio = ratio;
          registration.last_intersecting = intersecting;
        }
        registration.has_last = true;
        if (changed &&
            !entries->Set(context, entries->Length(), entry).FromMaybe(false)) {
          *error = "V8 failed to queue layout observer entry";
          return false;
        }
      }
      if (entries->Length() == 0)
        continue;
      delivered = true;
      v8::Local<v8::Function> callback = state->callback.Get(realm->isolate);
      v8::Local<v8::Object> observer = weak->object.Get(realm->isolate);
      v8::Local<v8::Value> arguments[] = {entries, observer};
      v8::TryCatch caught(realm->isolate);
      v8::Local<v8::Value> ignored;
      if (!callback->Call(context, v8::Undefined(realm->isolate), 2, arguments)
               .ToLocal(&ignored)) {
        *error = DescribeException(realm->isolate, context, caught);
        return false;
      }
    }
    if (!delivered)
      return true;
  }
  *error = "ResizeObserver delivery exceeded the loop limit";
  return false;
}

void ElementGetBoundingClientRect(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  gossamer_v8_element_geometry geometry{};
  std::string error;
  if (!ReadElementGeometry(realm, key, &geometry, &error)) {
    ThrowError(isolate, error);
    return;
  }
  v8::Local<v8::Object> rect;
  if (!CreateDOMRect(realm, isolate->GetCurrentContext(), geometry.rect)
           .ToLocal(&rect)) {
    ThrowError(isolate, "V8 failed to allocate DOMRect");
    return;
  }
  info.GetReturnValue().Set(rect);
}

void ElementGetClientRects(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  size_t count = 0;
  std::string error;
  if (!ReadElementClientRectCount(realm, key, &count, &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (count > static_cast<size_t>(std::numeric_limits<int>::max())) {
    ThrowError(isolate, "element client rect list is too large");
    return;
  }
  v8::Local<v8::Array> rects =
      v8::Array::New(isolate, static_cast<int>(count));
  for (size_t index = 0; index < count; ++index) {
    gossamer_v8_rect geometry{};
    bool found = false;
    if (!ReadElementClientRect(realm, key, index, &geometry, &found, &error)) {
      ThrowError(isolate, error);
      return;
    }
    if (!found) {
      ThrowError(isolate, "element client rect list changed during read");
      return;
    }
    v8::Local<v8::Object> rect;
    if (!CreateDOMRect(realm, isolate->GetCurrentContext(), geometry)
             .ToLocal(&rect) ||
        !rects
             ->Set(isolate->GetCurrentContext(), static_cast<uint32_t>(index),
                   rect)
             .FromMaybe(false)) {
      ThrowError(isolate, "V8 failed to allocate client rect list");
      return;
    }
  }
  info.GetReturnValue().Set(rects);
}

void ElementGeometryGetter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key))
    return;
  gossamer_v8_element_geometry geometry{};
  std::string error;
  if (!ReadElementGeometry(realm, key, &geometry, &error)) {
    ThrowError(isolate, error);
    return;
  }
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  double value = 0;
  bool integer = true;
  if (name == "clientWidth")
    value = geometry.client_width;
  else if (name == "clientHeight")
    value = geometry.client_height;
  else if (name == "offsetWidth")
    value = geometry.offset_width;
  else if (name == "offsetHeight")
    value = geometry.offset_height;
  else if (name == "scrollWidth")
    value = geometry.scroll_width;
  else if (name == "scrollHeight")
    value = geometry.scroll_height;
  else if (name == "scrollLeft") {
    value = geometry.scroll_left;
    integer = false;
  } else if (name == "scrollTop") {
    value = geometry.scroll_top;
    integer = false;
  }
  if (integer)
    value = std::round(value);
  info.GetReturnValue().Set(v8::Number::New(isolate, value));
}

bool ScrollElementHost(gossamer_v8_realm *realm, const WrapperKey &key,
                       double x, double y, std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  int changed = 0;
  char *host_error = nullptr;
  if (realm->active_host->scroll_element(
          realm->active_host->execution_id, key.document, key.node, x, y,
          &changed, &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "scrolling element failed";
    return false;
  }
  std::free(host_error);
  return true;
}

void ElementScrollSetter(
    v8::Local<v8::Name> property, v8::Local<v8::Value> input,
    const v8::PropertyCallbackInfo<v8::Boolean> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.Holder(), &key)) {
    info.GetReturnValue().Set(false);
    return;
  }
  gossamer_v8_element_geometry geometry{};
  std::string error;
  if (!ReadElementGeometry(realm, key, &geometry, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  double value = 0;
  if (!input->NumberValue(isolate->GetCurrentContext()).To(&value)) {
    info.GetReturnValue().Set(false);
    return;
  }
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  double x = name == "scrollLeft" ? value : geometry.scroll_left;
  double y = name == "scrollTop" ? value : geometry.scroll_top;
  if (!ScrollElementHost(realm, key, x, y, &error)) {
    ThrowError(isolate, error);
    info.GetReturnValue().Set(false);
    return;
  }
  info.GetReturnValue().Set(true);
}

bool ReadScrollCoordinates(const v8::FunctionCallbackInfo<v8::Value> &info,
                           double fallback_x, double fallback_y, double *x,
                           double *y) {
  v8::Local<v8::Context> context = info.GetIsolate()->GetCurrentContext();
  *x = 0;
  *y = 0;
  if (info.Length() == 0)
    return true;
  if (info[0]->IsObject() && !info[0]->IsNull()) {
    v8::Local<v8::Object> options = info[0].As<v8::Object>();
    return ReadNumberOption(context, options, "left", fallback_x, x) &&
           ReadNumberOption(context, options, "top", fallback_y, y);
  }
  if (!info[0]->NumberValue(context).To(x))
    return false;
  if (info.Length() > 1 && !info[1]->NumberValue(context).To(y))
    return false;
  return true;
}

void ElementScroll(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  WrapperKey key;
  if (!ReadReceiverKey(isolate, info.This(), &key))
    return;
  gossamer_v8_element_geometry geometry{};
  std::string error;
  if (!ReadElementGeometry(realm, key, &geometry, &error)) {
    ThrowError(isolate, error);
    return;
  }
  double x = 0;
  double y = 0;
  if (!ReadScrollCoordinates(info, geometry.scroll_left, geometry.scroll_top,
                             &x, &y))
    return;
  bool relative = info.Data()->IsBoolean() && info.Data()->BooleanValue(isolate);
  if (relative) {
    x += geometry.scroll_left;
    y += geometry.scroll_top;
  }
  if (!ScrollElementHost(realm, key, x, y, &error))
    ThrowError(isolate, error);
}

void ElementScrollIntoView(const v8::FunctionCallbackInfo<v8::Value> &info) {
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
  int changed = 0;
  char *host_error = nullptr;
  if (realm->active_host->scroll_into_view(
          realm->active_host->execution_id, key.document, key.node, &changed,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "scrollIntoView failed" : error);
    return;
  }
  std::free(host_error);
}

void GlobalViewportGetter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_viewport_geometry geometry{};
  std::string error;
  if (!ReadViewportGeometry(CurrentRealm(isolate), &geometry, &error)) {
    ThrowError(isolate, error);
    return;
  }
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  double value = 0;
  if (name == "innerWidth")
    value = geometry.inner_width;
  else if (name == "innerHeight")
    value = geometry.inner_height;
  else if (name == "scrollX" || name == "pageXOffset")
    value = geometry.scroll_x;
  else if (name == "scrollY" || name == "pageYOffset")
    value = geometry.scroll_y;
  info.GetReturnValue().Set(v8::Number::New(isolate, value));
}

bool ScrollViewportHost(gossamer_v8_realm *realm, double x, double y,
                        std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  int changed = 0;
  char *host_error = nullptr;
  if (realm->active_host->scroll_viewport(realm->active_host->execution_id, x,
                                           y, &changed, &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "scrolling viewport failed";
    return false;
  }
  std::free(host_error);
  return true;
}

void WindowScroll(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  gossamer_v8_viewport_geometry geometry{};
  std::string error;
  if (!ReadViewportGeometry(realm, &geometry, &error)) {
    ThrowError(isolate, error);
    return;
  }
  double x = 0;
  double y = 0;
  if (!ReadScrollCoordinates(info, geometry.scroll_x, geometry.scroll_y, &x,
                             &y))
    return;
  bool relative = info.Data()->IsBoolean() && info.Data()->BooleanValue(isolate);
  if (relative) {
    x += geometry.scroll_x;
    y += geometry.scroll_y;
  }
  if (!ScrollViewportHost(realm, x, y, &error))
    ThrowError(isolate, error);
}

struct SessionHistorySnapshotValue {
  int32_t length = 0;
  int32_t index = -1;
  std::string state_json;
  std::string url;
};

bool ReadSessionHistorySnapshot(gossamer_v8_realm *realm,
                                SessionHistorySnapshotValue *snapshot,
                                std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *state_json = nullptr;
  size_t state_json_length = 0;
  char *url = nullptr;
  size_t url_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->session_history_snapshot(
          realm->active_host->execution_id, &snapshot->length,
          &snapshot->index, &state_json, &state_json_length, &url,
          &url_length, &host_error) == 0) {
    *error = TakeCString(host_error);
    std::free(state_json);
    std::free(url);
    if (error->empty())
      *error = "reading session history failed";
    return false;
  }
  std::free(host_error);
  snapshot->state_json.assign(state_json == nullptr ? "" : state_json,
                              state_json_length);
  snapshot->url.assign(url == nullptr ? "" : url, url_length);
  std::free(state_json);
  std::free(url);
  return true;
}

void HistoryPropertyGetter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  std::string name = UTF8Value(isolate, property.As<v8::Value>());
  if (name == "scrollRestoration") {
    v8::Local<v8::String> value;
    if (NewUTF8String(isolate, realm->scroll_restoration.data(),
                      realm->scroll_restoration.size(), &value))
      info.GetReturnValue().Set(value);
    return;
  }
  SessionHistorySnapshotValue snapshot;
  std::string error;
  if (!ReadSessionHistorySnapshot(realm, &snapshot, &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (name == "length") {
    info.GetReturnValue().Set(snapshot.length);
    return;
  }
  v8::Local<v8::String> state_json;
  v8::Local<v8::Value> state;
  const std::string &encoded = snapshot.state_json.empty()
                                   ? std::string("null")
                                   : snapshot.state_json;
  if (!NewUTF8String(isolate, encoded.data(), encoded.size(), &state_json) ||
      !v8::JSON::Parse(isolate->GetCurrentContext(), state_json)
           .ToLocal(&state)) {
    ThrowError(isolate, "V8 failed to decode history.state");
    return;
  }
  info.GetReturnValue().Set(state);
}

void HistoryScrollRestorationSetter(
    v8::Local<v8::Name>, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<void> &info) {
  std::string rendered = UTF8Value(info.GetIsolate(), value);
  if (rendered == "auto" || rendered == "manual")
    CurrentRealm(info.GetIsolate())->scroll_restoration = rendered;
}

void HistoryUpdateState(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Value> state =
      info.Length() > 0 ? info[0] : v8::Undefined(isolate);
  std::string encoded = "null";
  if (!state->IsUndefined()) {
    v8::Local<v8::String> json;
    bool serialized = false;
    {
      v8::TryCatch caught(isolate);
      serialized = v8::JSON::Stringify(context, state).ToLocal(&json);
      if (!serialized)
        caught.Reset();
    }
    if (!serialized) {
      ThrowDOMException(isolate, "DataCloneError",
                        "history state is not JSON serializable");
      return;
    }
    encoded = UTF8Value(isolate, json);
  }
  std::string url;
  if (info.Length() > 2 && !info[2]->IsUndefined()) {
    v8::Local<v8::String> rendered;
    if (!info[2]->ToString(context).ToLocal(&rendered))
      return;
    url = UTF8Value(isolate, rendered);
  }
  bool replace = info.Data()->BooleanValue(isolate);
  int changed = 0;
  char *host_error = nullptr;
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  if (realm->active_host->update_history_state(
          realm->active_host->execution_id, encoded.data(), encoded.size(),
          url.data(), url.size(), replace ? 1 : 0, &changed,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "updating session history failed"
                                      : error);
    return;
  }
  std::free(host_error);
}

void HistoryTraverse(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  int32_t delta = 0;
  if (info.Data()->IsInt32()) {
    delta = info.Data().As<v8::Int32>()->Value();
  } else if (info.Length() > 0) {
    delta = info[0]
                ->Int32Value(isolate->GetCurrentContext())
                .FromMaybe(0);
  }
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->traverse_history(
          realm->active_host->execution_id, delta, &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "traversing session history failed"
                                      : error);
    return;
  }
  std::free(host_error);
}

uint8_t LocationComponentFromName(const std::string &name) {
  if (name == "href")
    return 1;
  if (name == "origin")
    return 2;
  if (name == "protocol")
    return 3;
  if (name == "host")
    return 4;
  if (name == "hostname")
    return 5;
  if (name == "port")
    return 6;
  if (name == "pathname")
    return 7;
  if (name == "search")
    return 8;
  if (name == "hash")
    return 9;
  return 0;
}

void LocationPropertyGetter(
    v8::Local<v8::Name> property,
    const v8::PropertyCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  uint8_t component =
      LocationComponentFromName(UTF8Value(isolate, property.As<v8::Value>()));
  std::string error;
  if (component == 0 || !RequireHost(realm, &error)) {
    ThrowError(isolate, component == 0 ? "invalid Location property" : error);
    return;
  }
  char *value = nullptr;
  size_t value_length = 0;
  char *host_error = nullptr;
  if (realm->active_host->location_component(
          realm->active_host->execution_id, component, &value, &value_length,
          &host_error) == 0) {
    error = TakeCString(host_error);
    std::free(value);
    ThrowError(isolate, error.empty() ? "reading Location failed" : error);
    return;
  }
  std::free(host_error);
  v8::Local<v8::String> rendered;
  bool allocated = NewUTF8String(isolate, value == nullptr ? "" : value,
                                 value_length, &rendered);
  std::free(value);
  if (!allocated) {
    ThrowError(isolate, "V8 failed to allocate Location value");
    return;
  }
  info.GetReturnValue().Set(rendered);
}

bool SetLocationComponentHost(gossamer_v8_realm *realm, uint8_t component,
                              const std::string &value,
                              std::string *error) {
  if (!RequireHost(realm, error))
    return false;
  char *host_error = nullptr;
  if (realm->active_host->set_location_component(
          realm->active_host->execution_id, component, value.data(),
          value.size(), &host_error) == 0) {
    *error = TakeCString(host_error);
    if (error->empty())
      *error = "updating Location failed";
    return false;
  }
  std::free(host_error);
  return true;
}

void LocationPropertySetter(
    v8::Local<v8::Name> property, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<void> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  uint8_t component =
      LocationComponentFromName(UTF8Value(isolate, property.As<v8::Value>()));
  if (component == 2)
    return;
  v8::Local<v8::String> rendered;
  if (!value->ToString(isolate->GetCurrentContext()).ToLocal(&rendered))
    return;
  std::string error;
  if (!SetLocationComponentHost(CurrentRealm(isolate), component,
                                UTF8Value(isolate, rendered), &error))
    ThrowError(isolate, error);
}

void LocationNavigate(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  gossamer_v8_realm *realm = CurrentRealm(isolate);
  uint8_t action = static_cast<uint8_t>(info.Data().As<v8::Int32>()->Value());
  std::string value;
  if (action != 3) {
    v8::Local<v8::String> rendered;
    if (info.Length() == 0 ||
        !info[0]->ToString(isolate->GetCurrentContext()).ToLocal(&rendered)) {
      ThrowError(isolate, "Location navigation requires a URL");
      return;
    }
    value = UTF8Value(isolate, rendered);
  }
  std::string error;
  if (!RequireHost(realm, &error)) {
    ThrowError(isolate, error);
    return;
  }
  char *host_error = nullptr;
  if (realm->active_host->navigate_location(
          realm->active_host->execution_id, value.data(), value.size(), action,
          &host_error) == 0) {
    error = TakeCString(host_error);
    ThrowError(isolate, error.empty() ? "Location navigation failed" : error);
    return;
  }
  std::free(host_error);
}

void LocationToString(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Local<v8::Value> href;
  if (info.This()
          ->Get(info.GetIsolate()->GetCurrentContext(),
                v8::String::NewFromUtf8Literal(info.GetIsolate(), "href"))
          .ToLocal(&href))
    info.GetReturnValue().Set(href);
}

void GlobalLocationGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  gossamer_v8_realm *realm = CurrentRealm(info.GetIsolate());
  if (!realm->location_object.IsEmpty())
    info.GetReturnValue().Set(realm->location_object.Get(info.GetIsolate()));
}

void GlobalLocationSetter(
    v8::Local<v8::Name>, v8::Local<v8::Value> value,
    const v8::PropertyCallbackInfo<void> &info) {
  v8::Local<v8::String> rendered;
  if (!value->ToString(info.GetIsolate()->GetCurrentContext())
           .ToLocal(&rendered))
    return;
  std::string error;
  if (!SetLocationComponentHost(CurrentRealm(info.GetIsolate()), 1,
                                UTF8Value(info.GetIsolate(), rendered),
                                &error))
    ThrowError(info.GetIsolate(), error);
}

void DocumentLocationGetter(
    v8::Local<v8::Name>, const v8::PropertyCallbackInfo<v8::Value> &info) {
  GlobalLocationGetter(v8::Local<v8::Name>(), info);
}

void IllegalDOMConstructor(
    const v8::FunctionCallbackInfo<v8::Value> &info) {
  ThrowError(info.GetIsolate(), "Illegal constructor");
}

bool InstallURLSearchParams(v8::Local<v8::Context> context) {
  static constexpr const char source[] = R"JS(
(function(global) {
  const state = new WeakMap();
  const searchParamOwners = new WeakMap();
  const text = value => String(value);
  const decode = value => {
    try { return decodeURIComponent(value.replace(/\+/g, " ")); }
    catch (_) { return value.replace(/\+/g, " "); }
  };
  const encode = value => encodeURIComponent(value)
    .replace(/%20/g, "+")
    .replace(/[!'()~]/g, character => "%" + character.charCodeAt(0).toString(16).toUpperCase());
  const require = object => {
    const pairs = state.get(object);
    if (!pairs) throw new TypeError("Illegal invocation");
    return pairs;
  };
  const renderSearchParams = object => require(object).map(pair => encode(pair[0]) + "=" + encode(pair[1])).join("&");
  const notifySearchParamsOwner = object => {
    const owner = searchParamOwners.get(object);
    if (!owner) return;
    const query = renderSearchParams(object);
    owner.search = query ? "?" + query : "";
  };
  class URLSearchParams {
    constructor(initial = "") {
      const pairs = [];
      state.set(this, pairs);
      if (initial instanceof URLSearchParams) {
        for (const pair of require(initial)) pairs.push(pair.slice());
      } else if (Array.isArray(initial)) {
        for (const pair of initial) {
          if (!pair || pair.length !== 2) throw new TypeError("Each query pair must contain two values");
          pairs.push([text(pair[0]), text(pair[1])]);
        }
      } else if (initial !== null && typeof initial === "object") {
        for (const key of Object.keys(initial)) pairs.push([key, text(initial[key])]);
      } else {
        let query = text(initial);
        if (query[0] === "?") query = query.slice(1);
        if (query !== "") for (const part of query.split("&")) {
          const separator = part.indexOf("=");
          pairs.push(separator < 0
            ? [decode(part), ""]
            : [decode(part.slice(0, separator)), decode(part.slice(separator + 1))]);
        }
      }
    }
    append(name, value) { require(this).push([text(name), text(value)]); notifySearchParamsOwner(this); }
    delete(name, value) {
      name = text(name);
      const matchValue = arguments.length > 1;
      if (matchValue) value = text(value);
      const pairs = require(this);
      for (let index = pairs.length - 1; index >= 0; --index)
        if (pairs[index][0] === name && (!matchValue || pairs[index][1] === value)) pairs.splice(index, 1);
      notifySearchParamsOwner(this);
    }
    get(name) {
      name = text(name);
      const pair = require(this).find(pair => pair[0] === name);
      return pair ? pair[1] : null;
    }
    getAll(name) { name = text(name); return require(this).filter(pair => pair[0] === name).map(pair => pair[1]); }
    has(name, value) {
      name = text(name);
      const matchValue = arguments.length > 1;
      if (matchValue) value = text(value);
      return require(this).some(pair => pair[0] === name && (!matchValue || pair[1] === value));
    }
    set(name, value) {
      name = text(name); value = text(value);
      const pairs = require(this);
      let found = false;
      for (let index = 0; index < pairs.length;) {
        if (pairs[index][0] !== name) { ++index; continue; }
        if (!found) { pairs[index][1] = value; found = true; ++index; }
        else pairs.splice(index, 1);
      }
      if (!found) pairs.push([name, value]);
      notifySearchParamsOwner(this);
    }
    sort() { require(this).sort((left, right) => left[0] < right[0] ? -1 : left[0] > right[0] ? 1 : 0); notifySearchParamsOwner(this); }
    toString() { return renderSearchParams(this); }
    *keys() { for (const pair of require(this)) yield pair[0]; }
    *values() { for (const pair of require(this)) yield pair[1]; }
    *entries() { for (const pair of require(this)) yield pair.slice(); }
    forEach(callback, thisArg) { for (const pair of require(this)) callback.call(thisArg, pair[1], pair[0], this); }
    get size() { return require(this).length; }
    [Symbol.iterator]() { return this.entries(); }
  }
  Object.defineProperty(URLSearchParams.prototype, Symbol.toStringTag, {value: "URLSearchParams", configurable: true});
  global.URLSearchParams = URLSearchParams;

  const urlState = new WeakMap();
  const parseURL = (input, base) => {
    input = String(input);
    if (!/^[A-Za-z][A-Za-z0-9+.-]*:/.test(input)) {
      if (base === undefined) throw new TypeError("Invalid URL");
      const parent = parseURL(String(base));
      const parentAuthority = parent.protocol + "//" +
        (parent.username || parent.password ? parent.username + (parent.password ? ":" + parent.password : "") + "@" : "") +
        parent.host;
      if (input.startsWith("//")) input = parent.protocol + input;
      else if (input.startsWith("/")) input = parentAuthority + input;
      else if (input.startsWith("?") || input.startsWith("#")) input = parentAuthority + parent.pathname + input;
      else input = parentAuthority + parent.pathname.replace(/[^/]*$/, "") + input;
    }
    const match = /^([A-Za-z][A-Za-z0-9+.-]*:)(?:\/\/([^/?#]*))?([^?#]*)(\?[^#]*)?(#.*)?$/.exec(input);
    if (!match) throw new TypeError("Invalid URL");
    const protocol = match[1].toLowerCase();
    let authority = match[2] || "";
    let username = "", password = "";
    const at = authority.lastIndexOf("@");
    if (at >= 0) {
      const user = authority.slice(0, at).split(":");
      username = user.shift() || ""; password = user.join(":"); authority = authority.slice(at + 1);
    }
    let hostname = authority, port = "";
    const portMatch = /^(\[[^\]]+\]|[^:]*)(?::([0-9]*))?$/.exec(authority);
    if (portMatch) { hostname = portMatch[1]; port = portMatch[2] || ""; }
    let pathname = match[3] || "/";
    const trailingSlash = pathname.endsWith("/") || pathname.endsWith("/.") || pathname.endsWith("/..");
    const segments = [];
    for (const segment of pathname.split("/")) {
      if (!segment || segment === ".") continue;
      if (segment === "..") segments.pop(); else segments.push(segment);
    }
    pathname = "/" + segments.join("/") + (trailingSlash && segments.length ? "/" : "");
    const host = hostname + (port ? ":" + port : "");
    const origin = ["http:", "https:", "ws:", "wss:"].includes(protocol) ? protocol + "//" + host : "null";
    return {protocol, username, password, host, hostname, port, pathname, search: match[4] || "", hash: match[5] || "", origin};
  };
  const renderURL = value => value.protocol + "//" +
    (value.username || value.password ? value.username + (value.password ? ":" + value.password : "") + "@" : "") +
    value.host + value.pathname + value.search + value.hash;
  const syncSearchParams = value => {
    if (!value.searchParams) return;
    const parsed = new URLSearchParams(value.search);
    const target = require(value.searchParams);
    target.splice(0, target.length, ...require(parsed).map(pair => pair.slice()));
  };
  class URL {
    constructor(input, base) { urlState.set(this, parseURL(input, base)); }
    static canParse(input, base) { try { parseURL(input, base); return true; } catch (_) { return false; } }
    toString() { return this.href; }
    toJSON() { return this.href; }
    get href() { const value = urlState.get(this); if (!value) throw new TypeError("Illegal invocation"); return renderURL(value); }
    set href(input) {
      const previous = urlState.get(this);
      const next = parseURL(input);
      if (previous && previous.searchParams) {
        next.searchParams = previous.searchParams;
        searchParamOwners.set(next.searchParams, next);
        syncSearchParams(next);
      }
      urlState.set(this, next);
    }
    get origin() { return urlState.get(this).origin; }
    get protocol() { return urlState.get(this).protocol; }
    set protocol(value) { const state = urlState.get(this); state.protocol = String(value).replace(/:?$/, ":"); state.origin = state.protocol + "//" + state.host; }
    get username() { return urlState.get(this).username; }
    set username(value) { urlState.get(this).username = String(value); }
    get password() { return urlState.get(this).password; }
    set password(value) { urlState.get(this).password = String(value); }
    get host() { return urlState.get(this).host; }
    set host(value) { const state = urlState.get(this); const parsed = parseURL(state.protocol + "//" + value + state.pathname); state.host = parsed.host; state.hostname = parsed.hostname; state.port = parsed.port; state.origin = state.protocol + "//" + state.host; }
    get hostname() { return urlState.get(this).hostname; }
    set hostname(value) { const state = urlState.get(this); state.hostname = String(value); state.host = state.hostname + (state.port ? ":" + state.port : ""); state.origin = state.protocol + "//" + state.host; }
    get port() { return urlState.get(this).port; }
    set port(value) { const state = urlState.get(this); state.port = String(value); state.host = state.hostname + (state.port ? ":" + state.port : ""); state.origin = state.protocol + "//" + state.host; }
    get pathname() { return urlState.get(this).pathname; }
    set pathname(value) { urlState.get(this).pathname = String(value).startsWith("/") ? String(value) : "/" + value; }
    get search() { return urlState.get(this).search; }
    set search(value) {
      value = String(value);
      const current = urlState.get(this);
      current.search = value && !value.startsWith("?") ? "?" + value : value;
      syncSearchParams(current);
    }
    get searchParams() {
      const current = urlState.get(this);
      if (!current.searchParams) {
        current.searchParams = new URLSearchParams(current.search);
        searchParamOwners.set(current.searchParams, current);
      }
      return current.searchParams;
    }
    get hash() { return urlState.get(this).hash; }
    set hash(value) { value = String(value); urlState.get(this).hash = value && !value.startsWith("#") ? "#" + value : value; }
  }
  Object.defineProperty(URL.prototype, Symbol.toStringTag, {value: "URL", configurable: true});
  global.URL = URL;

  const utf8Encode = input => {
    input = String(input);
    const bytes = [];
    for (let index = 0; index < input.length; ++index) {
      let point = input.charCodeAt(index);
      if (point >= 0xD800 && point <= 0xDBFF) {
        const low = input.charCodeAt(index + 1);
        if (low >= 0xDC00 && low <= 0xDFFF) {
          point = 0x10000 + ((point - 0xD800) << 10) + low - 0xDC00;
          ++index;
        } else point = 0xFFFD;
      } else if (point >= 0xDC00 && point <= 0xDFFF) point = 0xFFFD;
      if (point <= 0x7F) bytes.push(point);
      else if (point <= 0x7FF) bytes.push(0xC0 | point >> 6, 0x80 | point & 0x3F);
      else if (point <= 0xFFFF) bytes.push(0xE0 | point >> 12, 0x80 | point >> 6 & 0x3F, 0x80 | point & 0x3F);
      else bytes.push(0xF0 | point >> 18, 0x80 | point >> 12 & 0x3F, 0x80 | point >> 6 & 0x3F, 0x80 | point & 0x3F);
    }
    return bytes;
  };
  class TextEncoder {
    get encoding() { return "utf-8"; }
    encode(input = "") { return Uint8Array.from(utf8Encode(input)); }
    encodeInto(input, destination) {
      if (!(destination instanceof Uint8Array)) throw new TypeError("destination must be a Uint8Array");
      input = String(input);
      let read = 0, written = 0;
      while (read < input.length) {
        const width = input.codePointAt(read) > 0xFFFF ? 2 : 1;
        const bytes = utf8Encode(input.slice(read, read + width));
        if (written + bytes.length > destination.length) break;
        destination.set(bytes, written);
        read += width; written += bytes.length;
      }
      return {read, written};
    }
  }
  Object.defineProperty(TextEncoder.prototype, Symbol.toStringTag, {value: "TextEncoder", configurable: true});
  global.TextEncoder = TextEncoder;

  const decoderState = new WeakMap();
  const utf8Decode = (bytes, fatal) => {
    let result = "";
    for (let index = 0; index < bytes.length;) {
      const first = bytes[index++];
      let point, count, minimum;
      if (first <= 0x7F) { point = first; count = 0; minimum = 0; }
      else if (first >= 0xC2 && first <= 0xDF) { point = first & 0x1F; count = 1; minimum = 0x80; }
      else if (first >= 0xE0 && first <= 0xEF) { point = first & 0x0F; count = 2; minimum = 0x800; }
      else if (first >= 0xF0 && first <= 0xF4) { point = first & 0x07; count = 3; minimum = 0x10000; }
      else { if (fatal) throw new TypeError("invalid UTF-8 input"); result += "\uFFFD"; continue; }
      let valid = index + count <= bytes.length;
      for (let offset = 0; valid && offset < count; ++offset) {
        const next = bytes[index + offset];
        if ((next & 0xC0) !== 0x80) valid = false;
        else point = point << 6 | next & 0x3F;
      }
      if (!valid || point < minimum || point > 0x10FFFF || point >= 0xD800 && point <= 0xDFFF) {
        if (fatal) throw new TypeError("invalid UTF-8 input");
        result += "\uFFFD";
        continue;
      }
      index += count;
      result += String.fromCodePoint(point);
    }
    return result;
  };
  class TextDecoder {
    constructor(label = "utf-8", options = {}) {
      label = String(label).trim().toLowerCase();
      if (!["unicode-1-1-utf-8", "unicode11utf8", "unicode20utf8", "utf-8", "utf8", "x-unicode20utf8"].includes(label))
        throw new RangeError("unsupported encoding label");
      decoderState.set(this, {fatal: !!options.fatal, ignoreBOM: !!options.ignoreBOM});
    }
    get encoding() { if (!decoderState.has(this)) throw new TypeError("Illegal invocation"); return "utf-8"; }
    get fatal() { const value = decoderState.get(this); if (!value) throw new TypeError("Illegal invocation"); return value.fatal; }
    get ignoreBOM() { const value = decoderState.get(this); if (!value) throw new TypeError("Illegal invocation"); return value.ignoreBOM; }
    decode(input = new Uint8Array()) {
      const settings = decoderState.get(this);
      if (!settings) throw new TypeError("Illegal invocation");
      let bytes;
      if (input instanceof ArrayBuffer) bytes = new Uint8Array(input);
      else if (ArrayBuffer.isView(input)) bytes = new Uint8Array(input.buffer, input.byteOffset, input.byteLength);
      else throw new TypeError("input must be an ArrayBuffer or view");
      let result = utf8Decode(bytes, settings.fatal);
      if (!settings.ignoreBOM && result.charCodeAt(0) === 0xFEFF) result = result.slice(1);
      return result;
    }
  }
  Object.defineProperty(TextDecoder.prototype, Symbol.toStringTag, {value: "TextDecoder", configurable: true});
  global.TextDecoder = TextDecoder;

  const navigator = {
    userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Gossamer/0.1",
    appVersion: "5.0 (Macintosh; Intel Mac OS X 10_15_7) Gossamer/0.1",
    platform: "MacIntel",
    vendor: "",
    language: "en-US",
    languages: ["en-US"],
    hardwareConcurrency: 4,
    deviceMemory: 8,
    maxTouchPoints: 0,
    onLine: true,
    cookieEnabled: true,
    standalone: false
  };
  Object.defineProperty(global, "navigator", {value: navigator, enumerable: true, configurable: true});

  const mediaLength = token => {
    const match = /^(-?[0-9]+(?:\.[0-9]+)?)(px|em|rem|vw|vh)?$/.exec(token.trim());
    if (!match) return NaN;
    const number = Number(match[1]);
    switch (match[2] || "px") {
      case "em": case "rem": return number * 16;
      case "vw": return number * global.innerWidth / 100;
      case "vh": return number * global.innerHeight / 100;
      default: return number;
    }
  };
  const mediaFeatureMatches = (name, rawValue) => {
    name = name.trim().toLowerCase();
    const value = rawValue === undefined ? "" : rawValue.trim().toLowerCase();
    if (["width", "min-width", "max-width", "height", "min-height", "max-height"].includes(name)) {
      if (!value) return (name.includes("width") ? global.innerWidth : global.innerHeight) > 0;
      const expected = mediaLength(value);
      if (!Number.isFinite(expected)) return false;
      const actual = name.includes("width") ? global.innerWidth : global.innerHeight;
      if (name.startsWith("min-")) return actual >= expected;
      if (name.startsWith("max-")) return actual <= expected;
      return actual === expected;
    }
    if (name === "orientation") return value === "landscape" ? global.innerWidth > global.innerHeight : value === "portrait" && global.innerHeight >= global.innerWidth;
    if (name === "hover" || name === "any-hover" || name === "pointer" || name === "any-pointer") return value === "none";
    if (name === "prefers-color-scheme") return value === "light";
    if (name === "prefers-reduced-motion") return value === "no-preference";
    if (name === "display-mode") return value === "browser";
    if (name === "forced-colors") return value === "none";
    return false;
  };
  const mediaCandidateMatches = source => {
    let candidate = source.trim().toLowerCase();
    let negate = false;
    if (candidate.startsWith("not ")) { negate = true; candidate = candidate.slice(4).trim(); }
    if (candidate.startsWith("only ")) candidate = candidate.slice(5).trim();
    let result = true;
    const type = /^([a-z-]+)\b/.exec(candidate);
    if (type && !candidate.startsWith("(")) {
      result = type[1] === "screen" || type[1] === "all";
      candidate = candidate.slice(type[0].length).trim();
      if (candidate.startsWith("and")) candidate = candidate.slice(3).trim();
    }
    const conditions = [...candidate.matchAll(/\(([^:()]+)(?::\s*([^()]+))?\)/g)];
    if (candidate && conditions.length === 0) result = false;
    for (const condition of conditions) result = result && mediaFeatureMatches(condition[1], condition[2]);
    return negate ? !result : result;
  };
  class MediaQueryList {
    constructor(media) { this.media = String(media); this.matches = this.media.split(",").some(mediaCandidateMatches); this.onchange = null; }
    addListener() {}
    removeListener() {}
    addEventListener() {}
    removeEventListener() {}
    dispatchEvent() { return false; }
  }
  Object.defineProperty(MediaQueryList.prototype, Symbol.toStringTag, {value: "MediaQueryList", configurable: true});
  global.MediaQueryList = MediaQueryList;
  global.matchMedia = media => new MediaQueryList(media);

  function Image(width, height) {
    const image = global.document.createElement("img");
    if (width !== undefined) image.width = Number(width);
    if (height !== undefined) image.height = Number(height);
    return image;
  }
  Image.prototype = global.HTMLImageElement.prototype;
  global.Image = Image;

  const hostFetch = global.__gossamerHostFetch;
  delete global.__gossamerHostFetch;
  const hostStorage = global.__gossamerHostStorage;
  delete global.__gossamerHostStorage;
  const hostWebSocket = global.__gossamerHostWebSocket;
  delete global.__gossamerHostWebSocket;
  const storageCall = request => JSON.parse(hostStorage(JSON.stringify(request)));
  class Storage {
    constructor(area) { if (area !== 1 && area !== 2) throw new TypeError("Illegal constructor"); Object.defineProperty(this, "_area", {value: area}); }
    get length() { return storageCall({operation: "length", area: this._area}).length; }
    key(index) { const response = storageCall({operation: "key", area: this._area, index: Number(index)}); return response.found ? response.value : null; }
    getItem(key) { const response = storageCall({operation: "get", area: this._area, key: String(key)}); return response.found ? response.value : null; }
    setItem(key, value) { storageCall({operation: "set", area: this._area, key: String(key), value: String(value)}); }
    removeItem(key) { storageCall({operation: "remove", area: this._area, key: String(key)}); }
    clear() { storageCall({operation: "clear", area: this._area}); }
  }
  Object.defineProperty(Storage.prototype, Symbol.toStringTag, {value: "Storage", configurable: true});
  global.Storage = Storage;
  Object.defineProperty(global, "localStorage", {value: new Storage(1), enumerable: true, configurable: true});
  Object.defineProperty(global, "sessionStorage", {value: new Storage(2), enumerable: true, configurable: true});
  Object.defineProperty(global.Document.prototype, "cookie", {
    get() { return storageCall({operation: "cookie-get"}).value || ""; },
    set(value) { storageCall({operation: "cookie-set", value: String(value)}); },
    enumerable: true, configurable: true
  });
  const webSocketCall = request => JSON.parse(hostWebSocket(JSON.stringify(request)));
  const webSocketState = new WeakMap();
  const webSockets = new Map();
  const webSocketProtocols = value => {
    if (value === undefined) return [];
    const result = typeof value === "string" ? [value] : Array.from(value, String);
    const seen = new Set();
    for (const protocol of result) {
      if (!protocol || /[()<>@,;:\\"\/\[\]?={} \t\r\n]/.test(protocol)) throw new SyntaxError("Invalid WebSocket protocol");
      if (seen.has(protocol)) throw new SyntaxError("Duplicate WebSocket protocol");
      seen.add(protocol);
    }
    return result;
  };
  const webSocketRequire = socket => {
    const state = webSocketState.get(socket);
    if (!state) throw new TypeError("Illegal invocation");
    return state;
  };
  class WebSocket {
    constructor(url, protocols) {
      if (arguments.length === 0) throw new TypeError("WebSocket requires a URL");
      const requestedProtocols = webSocketProtocols(protocols);
      let resolvedURL = String(url);
      try { resolvedURL = new URL(resolvedURL, global.location.href).href; } catch (_) {}
      const response = webSocketCall({operation: "open", url: resolvedURL, protocols: requestedProtocols});
      const state = {id: response.id, readyState: 0, protocol: response.protocol || "", extensions: "", binaryType: "blob", listeners: new Map()};
      webSocketState.set(this, state);
      webSockets.set(response.id, this);
      Object.defineProperties(this, {
        url: {value: resolvedURL, enumerable: true},
        bufferedAmount: {value: 0, enumerable: true},
        onopen: {value: null, writable: true, enumerable: true},
        onmessage: {value: null, writable: true, enumerable: true},
        onerror: {value: null, writable: true, enumerable: true},
        onclose: {value: null, writable: true, enumerable: true}
      });
    }
    get readyState() { return webSocketRequire(this).readyState; }
    get protocol() { return webSocketRequire(this).protocol; }
    get extensions() { return webSocketRequire(this).extensions; }
    get binaryType() { return webSocketRequire(this).binaryType; }
    set binaryType(value) { value = String(value); if (value === "blob" || value === "arraybuffer") webSocketRequire(this).binaryType = value; }
    send(value) {
      const state = webSocketRequire(this);
      if (state.readyState !== 1) throw new Error("WebSocket is not open");
      let message = "text", data;
      if (typeof value === "string") data = Array.from(new TextEncoder().encode(value));
      else if (value instanceof ArrayBuffer) { message = "binary"; data = Array.from(new Uint8Array(value)); }
      else if (ArrayBuffer.isView(value)) { message = "binary"; data = Array.from(new Uint8Array(value.buffer, value.byteOffset, value.byteLength)); }
      else data = Array.from(new TextEncoder().encode(String(value)));
      webSocketCall({operation: "send", id: state.id, message, data});
    }
    close(code = 1000, reason = "") {
      const state = webSocketRequire(this);
      if (state.readyState === 2 || state.readyState === 3) return;
      code = Number(code); reason = String(reason);
      if (code !== 1000 && (code < 3000 || code > 4999)) throw new RangeError("Invalid WebSocket close code");
      if (new TextEncoder().encode(reason).length > 123) throw new SyntaxError("WebSocket close reason is too long");
      state.readyState = 2;
      webSocketCall({operation: "close", id: state.id, code, reason});
    }
    addEventListener(type, callback) {
      const state = webSocketRequire(this);
      if (typeof callback !== "function") return;
      type = String(type).toLowerCase();
      let listeners = state.listeners.get(type);
      if (!listeners) state.listeners.set(type, listeners = []);
      if (!listeners.includes(callback)) listeners.push(callback);
    }
    removeEventListener(type, callback) {
      const listeners = webSocketRequire(this).listeners.get(String(type).toLowerCase());
      if (!listeners) return;
      const index = listeners.indexOf(callback);
      if (index >= 0) listeners.splice(index, 1);
    }
  }
  for (const [name, value] of Object.entries({CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3})) {
    Object.defineProperty(WebSocket, name, {value});
    Object.defineProperty(WebSocket.prototype, name, {value});
  }
  Object.defineProperty(WebSocket.prototype, Symbol.toStringTag, {value: "WebSocket", configurable: true});
  Object.defineProperty(global, "__gossamerDispatchWebSocket", {value: eventJSON => {
    const wire = JSON.parse(eventJSON);
    const socket = webSockets.get(wire.id);
    if (!socket) return;
    const state = webSocketState.get(socket);
    if (wire.type === "open") {
      state.readyState = 1;
      state.protocol = wire.protocol || state.protocol;
      state.extensions = wire.extensions || "";
    } else if (wire.type === "close") state.readyState = 3;
    let data;
    if (wire.type === "message") {
      const bytes = Uint8Array.from(wire.data || []);
      data = wire.message === "text" ? new TextDecoder().decode(bytes) : bytes.buffer;
    }
    const event = {type: wire.type, target: socket, currentTarget: socket, data,
      code: wire.code || 0, reason: wire.reason || "", wasClean: !!wire.wasClean,
      message: wire.type === "error" ? wire.reason || "" : undefined};
    const handler = socket["on" + wire.type];
    if (typeof handler === "function") handler.call(socket, event);
    const listeners = state.listeners.get(wire.type);
    if (listeners) for (const callback of listeners.slice()) callback.call(socket, event);
    if (wire.type === "close") webSockets.delete(wire.id);
  }, configurable: false});
  global.WebSocket = WebSocket;
  const headerState = new WeakMap();
  const normalizeHeaderName = name => {
    name = String(name).trim().toLowerCase();
    if (!name || /[()<>@,;:\\"\/\[\]?={} \t\r\n]/.test(name)) throw new TypeError("Invalid HTTP header name");
    return name;
  };
  class Headers {
    constructor(initial) {
      const values = new Map();
      headerState.set(this, values);
      if (initial instanceof Headers) for (const [name, value] of initial) this.append(name, value);
      else if (initial != null && Symbol.iterator in Object(initial) && typeof initial !== "string") {
        for (const pair of initial) {
          if (!pair || pair.length !== 2) throw new TypeError("Header pair must contain two values");
          this.append(pair[0], pair[1]);
        }
      } else if (initial != null) for (const name of Object.keys(Object(initial))) {
        const value = initial[name];
        if (Array.isArray(value)) for (const item of value) this.append(name, item);
        else this.append(name, value);
      }
    }
    append(name, value) { name = normalizeHeaderName(name); value = String(value).trim(); const list = headerState.get(this); if (!list) throw new TypeError("Illegal invocation"); list.set(name, list.has(name) ? list.get(name) + ", " + value : value); }
    delete(name) { const list = headerState.get(this); if (!list) throw new TypeError("Illegal invocation"); list.delete(normalizeHeaderName(name)); }
    get(name) { const list = headerState.get(this); if (!list) throw new TypeError("Illegal invocation"); name = normalizeHeaderName(name); return list.has(name) ? list.get(name) : null; }
    has(name) { const list = headerState.get(this); if (!list) throw new TypeError("Illegal invocation"); return list.has(normalizeHeaderName(name)); }
    set(name, value) { const list = headerState.get(this); if (!list) throw new TypeError("Illegal invocation"); list.set(normalizeHeaderName(name), String(value).trim()); }
    *entries() { const list = headerState.get(this); if (!list) throw new TypeError("Illegal invocation"); yield* list.entries(); }
    *keys() { for (const pair of this) yield pair[0]; }
    *values() { for (const pair of this) yield pair[1]; }
    forEach(callback, thisArg) { for (const [name, value] of this) callback.call(thisArg, value, name, this); }
    [Symbol.iterator]() { return this.entries(); }
  }
  Object.defineProperty(Headers.prototype, Symbol.toStringTag, {value: "Headers", configurable: true});
  const requestState = new WeakMap();
  const bodyBytes = body => {
    if (body == null) return [];
    if (typeof body === "string") return Array.from(new TextEncoder().encode(body));
    if (body instanceof ArrayBuffer) return Array.from(new Uint8Array(body));
    if (ArrayBuffer.isView(body)) return Array.from(new Uint8Array(body.buffer, body.byteOffset, body.byteLength));
    return Array.from(new TextEncoder().encode(String(body)));
  };
  const wireHeaders = headers => {
    const result = {};
    for (const [name, value] of headers) result[name] = [value];
    return result;
  };
  class Request {
    constructor(input, init = {}) {
      const inherited = input instanceof Request ? requestState.get(input) : null;
      const url = inherited ? inherited.url : String(input);
      const method = String(init.method === undefined ? (inherited ? inherited.method : "GET") : init.method).toUpperCase();
      const headers = new Headers(init.headers === undefined ? (inherited ? inherited.headers : undefined) : init.headers);
      const body = init.body === undefined ? (inherited ? inherited.body.slice() : []) : bodyBytes(init.body);
      if ((method === "GET" || method === "HEAD") && body.length) throw new TypeError(method + " request cannot have a body");
      requestState.set(this, {url, method, headers, body});
      Object.defineProperties(this, {url: {value: url, enumerable: true}, method: {value: method, enumerable: true}, headers: {value: headers, enumerable: true}});
    }
    clone() { return new Request(this); }
  }
  Object.defineProperty(Request.prototype, Symbol.toStringTag, {value: "Request", configurable: true});
  const responseState = new WeakMap();
  class Response {
    constructor(body = null, init = {}) {
      const status = init.status === undefined ? 200 : Number(init.status);
      const statusText = init.statusText === undefined ? "" : String(init.statusText);
      const headers = new Headers(init.headers);
      responseState.set(this, {body: Uint8Array.from(bodyBytes(body)), used: false});
      Object.defineProperties(this, {
        status: {value: status, enumerable: true}, statusText: {value: statusText, enumerable: true},
        headers: {value: headers, enumerable: true}, ok: {value: status >= 200 && status <= 299, enumerable: true},
        url: {value: String(init.url || ""), enumerable: true}, redirected: {value: false, enumerable: true},
        type: {value: "basic", enumerable: true}
      });
    }
    get bodyUsed() { const state = responseState.get(this); if (!state) throw new TypeError("Illegal invocation"); return state.used; }
    _consume() { const state = responseState.get(this); if (!state) throw new TypeError("Illegal invocation"); if (state.used) throw new TypeError("Response body is already used"); state.used = true; return state.body; }
    text() { try { return Promise.resolve(new TextDecoder().decode(this._consume())); } catch (error) { return Promise.reject(error); } }
    json() { return this.text().then(JSON.parse); }
    arrayBuffer() { try { const bytes = this._consume(); return Promise.resolve(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength)); } catch (error) { return Promise.reject(error); } }
    clone() { const state = responseState.get(this); if (!state || state.used) throw new TypeError("Response body is already used"); return new Response(state.body, {status: this.status, statusText: this.statusText, headers: this.headers, url: this.url}); }
  }
  Object.defineProperty(Response.prototype, Symbol.toStringTag, {value: "Response", configurable: true});
  global.Headers = Headers;
  global.Request = Request;
  global.Response = Response;
  global.fetch = (input, init) => {
    try {
      const request = input instanceof Request && init === undefined ? input : new Request(input, init);
      const state = requestState.get(request);
      const response = JSON.parse(hostFetch(JSON.stringify({url: state.url, method: state.method, headers: wireHeaders(state.headers), body: state.body})));
      return Promise.resolve(new Response(Uint8Array.from(response.body), response));
    } catch (error) {
      return Promise.reject(error);
    }
  };

  let nextInterval = 1;
  const intervals = new Map();
  global.setInterval = (callback, delay = 0, ...arguments) => {
    if (typeof callback !== "function") throw new TypeError("setInterval requires a function");
    const interval = nextInterval++;
    const tick = () => {
      if (!intervals.has(interval)) return;
      callback(...arguments);
      if (intervals.has(interval)) intervals.set(interval, global.setTimeout(tick, delay));
    };
    intervals.set(interval, global.setTimeout(tick, delay));
    return interval;
  };
  global.clearInterval = interval => {
    const timer = intervals.get(Number(interval));
    if (timer !== undefined) global.clearTimeout(timer);
    intervals.delete(Number(interval));
  };
})(globalThis);
)JS";
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  v8::Local<v8::String> script_source;
  v8::Local<v8::Script> script;
  v8::Local<v8::Value> result;
  return v8::String::NewFromUtf8(isolate, source, v8::NewStringType::kNormal,
                                 static_cast<int>(sizeof(source) - 1))
             .ToLocal(&script_source) &&
         v8::Script::Compile(context, script_source).ToLocal(&script) &&
         script->Run(context).ToLocal(&result);
}

bool InstallBindings(gossamer_v8_realm *realm, v8::Local<v8::Context> context) {
  v8::Isolate *isolate = realm->isolate;
  v8::Local<v8::FunctionTemplate> dom_exception_template =
      v8::FunctionTemplate::New(isolate, DOMExceptionConstructor);
  v8::Local<v8::FunctionTemplate> event_target_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> node_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_form_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_input_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_text_area_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_select_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_option_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_button_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_template_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_iframe_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_head_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_script_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_media_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_image_element_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> text_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> document_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> document_fragment_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> history_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> location_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> mutation_observer_template =
      v8::FunctionTemplate::New(isolate, MutationObserverConstructor);
  v8::Local<v8::FunctionTemplate> mutation_record_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> resize_observer_template =
      v8::FunctionTemplate::New(isolate, LayoutObserverConstructor,
                                v8::False(isolate));
  v8::Local<v8::FunctionTemplate> intersection_observer_template =
      v8::FunctionTemplate::New(isolate, LayoutObserverConstructor,
                                v8::True(isolate));
  v8::Local<v8::FunctionTemplate> range_template =
      v8::FunctionTemplate::New(isolate, RangeConstructor);
  v8::Local<v8::FunctionTemplate> tree_walker_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> node_iterator_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> selection_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> node_list_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> html_collection_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> token_list_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> dataset_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> style_template =
      v8::FunctionTemplate::New(isolate, IllegalDOMConstructor);
  v8::Local<v8::FunctionTemplate> dom_rect_template =
      v8::FunctionTemplate::New(isolate, DOMRectConstructor);
  v8::Local<v8::FunctionTemplate> form_data_template =
      v8::FunctionTemplate::New(isolate, FormDataConstructor);
  auto new_event_template =
      [isolate](EventInterface interface) {
        return v8::FunctionTemplate::New(
            isolate, EventConstructor,
            v8::Integer::New(isolate, static_cast<int>(interface)));
      };
  v8::Local<v8::FunctionTemplate> event_template =
      new_event_template(EventInterface::Event);
  v8::Local<v8::FunctionTemplate> custom_event_template =
      new_event_template(EventInterface::CustomEvent);
  v8::Local<v8::FunctionTemplate> mouse_event_template =
      new_event_template(EventInterface::MouseEvent);
  v8::Local<v8::FunctionTemplate> pointer_event_template =
      new_event_template(EventInterface::PointerEvent);
  v8::Local<v8::FunctionTemplate> keyboard_event_template =
      new_event_template(EventInterface::KeyboardEvent);
  v8::Local<v8::FunctionTemplate> input_event_template =
      new_event_template(EventInterface::InputEvent);
  v8::Local<v8::FunctionTemplate> composition_event_template =
      new_event_template(EventInterface::CompositionEvent);
  v8::Local<v8::FunctionTemplate> focus_event_template =
      new_event_template(EventInterface::FocusEvent);

  dom_exception_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "DOMException"));
  dom_exception_template->PrototypeTemplate()->Set(
      isolate, "toString",
      v8::FunctionTemplate::New(isolate, DOMExceptionToString));
  dom_exception_template->PrototypeTemplate()->Set(
      v8::Symbol::GetToStringTag(isolate),
      v8::String::NewFromUtf8Literal(isolate, "DOMException"),
      static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontEnum));
  for (const auto &constant :
       {std::pair<const char *, int>{"INDEX_SIZE_ERR", 1},
        {"HIERARCHY_REQUEST_ERR", 3},
        {"INVALID_CHARACTER_ERR", 5},
        {"NOT_FOUND_ERR", 8},
        {"NOT_SUPPORTED_ERR", 9},
        {"INVALID_STATE_ERR", 11},
        {"SYNTAX_ERR", 12},
        {"NAMESPACE_ERR", 14},
        {"INVALID_NODE_TYPE_ERR", 24}}) {
    dom_exception_template->Set(
        v8::String::NewFromUtf8(isolate, constant.first).ToLocalChecked(),
        v8::Integer::New(isolate, constant.second),
        static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontDelete));
    dom_exception_template->PrototypeTemplate()->Set(
        v8::String::NewFromUtf8(isolate, constant.first).ToLocalChecked(),
        v8::Integer::New(isolate, constant.second),
        static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontDelete));
  }

  event_target_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "EventTarget"));
  node_template->SetClassName(v8::String::NewFromUtf8Literal(isolate, "Node"));
  element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "Element"));
  html_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLElement"));
  html_form_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLFormElement"));
  html_input_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLInputElement"));
  html_text_area_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLTextAreaElement"));
  html_select_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLSelectElement"));
  html_option_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLOptionElement"));
  html_button_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLButtonElement"));
  html_template_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLTemplateElement"));
  html_iframe_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLIFrameElement"));
  html_head_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLHeadElement"));
  html_script_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLScriptElement"));
  html_media_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLMediaElement"));
  html_image_element_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLImageElement"));
  for (const auto &constant :
       {std::pair<const char *, int>{"HAVE_NOTHING", 0},
        {"HAVE_METADATA", 1}, {"HAVE_CURRENT_DATA", 2},
        {"HAVE_FUTURE_DATA", 3}, {"HAVE_ENOUGH_DATA", 4}}) {
    html_media_element_template->Set(
        v8::String::NewFromUtf8(isolate, constant.first).ToLocalChecked(),
        v8::Integer::New(isolate, constant.second),
        static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontDelete));
    html_media_element_template->PrototypeTemplate()->Set(
        v8::String::NewFromUtf8(isolate, constant.first).ToLocalChecked(),
        v8::Integer::New(isolate, constant.second),
        static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontDelete));
  }
  text_template->SetClassName(v8::String::NewFromUtf8Literal(isolate, "Text"));
  document_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "Document"));
  document_fragment_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "DocumentFragment"));
  history_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "History"));
  location_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "Location"));
  mutation_observer_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "MutationObserver"));
  mutation_record_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "MutationRecord"));
  range_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "Range"));
  tree_walker_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "TreeWalker"));
  node_iterator_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "NodeIterator"));
  selection_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "Selection"));
  node_list_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "NodeList"));
  html_collection_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "HTMLCollection"));
  token_list_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "DOMTokenList"));
  dataset_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "DOMStringMap"));
  style_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "CSSStyleDeclaration"));
  dom_rect_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "DOMRect"));
  dom_rect_template->PrototypeTemplate()->Set(
      isolate, "toJSON", v8::FunctionTemplate::New(isolate, DOMRectToJSON));
  form_data_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "FormData"));
  form_data_template->InstanceTemplate()->SetInternalFieldCount(1);
  for (const auto &method :
       {std::pair<const char *, v8::FunctionCallback>{"append", FormDataAppend},
        {"delete", FormDataDelete}, {"get", FormDataGet},
        {"getAll", FormDataGetAll}, {"has", FormDataHas},
        {"set", FormDataSet}, {"entries", FormDataEntries},
        {"forEach", FormDataForEach}}) {
    form_data_template->PrototypeTemplate()->Set(
        isolate, method.first,
        v8::FunctionTemplate::New(isolate, method.second));
  }
  form_data_template->PrototypeTemplate()->Set(
      isolate, "keys",
      v8::FunctionTemplate::New(isolate, FormDataKeysOrValues,
                                v8::Boolean::New(isolate, true)));
  form_data_template->PrototypeTemplate()->Set(
      isolate, "values",
      v8::FunctionTemplate::New(isolate, FormDataKeysOrValues,
                                v8::Boolean::New(isolate, false)));
  form_data_template->PrototypeTemplate()->Set(
      v8::Symbol::GetIterator(isolate),
      v8::FunctionTemplate::New(isolate, FormDataEntries));
  form_data_template->PrototypeTemplate()->Set(
      v8::Symbol::GetToStringTag(isolate),
      v8::String::NewFromUtf8Literal(isolate, "FormData"),
      static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontEnum));
  event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "Event"));
  custom_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "CustomEvent"));
  mouse_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "MouseEvent"));
  pointer_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "PointerEvent"));
  keyboard_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "KeyboardEvent"));
  input_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "InputEvent"));
  composition_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "CompositionEvent"));
  focus_event_template->SetClassName(
      v8::String::NewFromUtf8Literal(isolate, "FocusEvent"));
  node_template->Inherit(event_target_template);
  element_template->Inherit(node_template);
  html_element_template->Inherit(element_template);
  for (v8::Local<v8::FunctionTemplate> specialized_template :
       {html_form_element_template, html_input_element_template,
        html_text_area_element_template, html_select_element_template,
        html_option_element_template, html_button_element_template,
        html_template_element_template, html_iframe_element_template,
        html_head_element_template, html_script_element_template,
        html_media_element_template, html_image_element_template}) {
    specialized_template->Inherit(html_element_template);
  }
  text_template->Inherit(node_template);
  document_template->Inherit(node_template);
  document_fragment_template->Inherit(node_template);
  mouse_event_template->Inherit(event_template);
  custom_event_template->Inherit(event_template);
  pointer_event_template->Inherit(mouse_event_template);
  keyboard_event_template->Inherit(event_template);
  input_event_template->Inherit(event_template);
  composition_event_template->Inherit(event_template);
  focus_event_template->Inherit(event_template);
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {node_template, element_template, html_element_template, text_template,
        document_template, document_fragment_template,
        html_form_element_template, html_input_element_template,
        html_text_area_element_template, html_select_element_template,
        html_option_element_template, html_button_element_template,
        html_template_element_template, html_iframe_element_template,
        html_head_element_template, html_script_element_template,
        html_media_element_template, html_image_element_template}) {
    interface_template->InstanceTemplate()->SetInternalFieldCount(
        kNodeInternalFieldCount);
  }
  for (v8::Local<v8::FunctionTemplate> facade_template :
       {node_list_template, html_collection_template, token_list_template,
        dataset_template}) {
    facade_template->InstanceTemplate()->SetInternalFieldCount(
        kFacadeInternalFieldCount);
  }
  style_template->InstanceTemplate()->SetInternalFieldCount(
      kStyleInternalFieldCount);
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {event_template, custom_event_template, mouse_event_template, pointer_event_template,
        keyboard_event_template, input_event_template,
        composition_event_template, focus_event_template}) {
    interface_template->InstanceTemplate()->SetInternalFieldCount(1);
  }
  mutation_observer_template->InstanceTemplate()->SetInternalFieldCount(1);
  mutation_observer_template->PrototypeTemplate()->Set(
      isolate, "observe",
      v8::FunctionTemplate::New(isolate, MutationObserverObserve));
  mutation_observer_template->PrototypeTemplate()->Set(
      isolate, "disconnect",
      v8::FunctionTemplate::New(isolate, MutationObserverDisconnect));
  mutation_observer_template->PrototypeTemplate()->Set(
      isolate, "takeRecords",
      v8::FunctionTemplate::New(isolate, MutationObserverTakeRecords));
  for (v8::Local<v8::FunctionTemplate> observer_template :
       {resize_observer_template, intersection_observer_template}) {
    observer_template->InstanceTemplate()->SetInternalFieldCount(1);
    observer_template->PrototypeTemplate()->Set(
        isolate, "observe",
        v8::FunctionTemplate::New(isolate, LayoutObserverObserve));
    observer_template->PrototypeTemplate()->Set(
        isolate, "unobserve",
        v8::FunctionTemplate::New(isolate, LayoutObserverUnobserve));
    observer_template->PrototypeTemplate()->Set(
        isolate, "disconnect",
        v8::FunctionTemplate::New(isolate, LayoutObserverDisconnect));
    observer_template->PrototypeTemplate()->Set(
        isolate, "takeRecords",
        v8::FunctionTemplate::New(isolate, LayoutObserverTakeRecords));
  }
  range_template->InstanceTemplate()->SetInternalFieldCount(1);
  for (const auto &property :
       {std::pair<const char *, int>{"startContainer", 1},
        {"startOffset", 2}, {"endContainer", 3}, {"endOffset", 4},
        {"collapsed", 5}, {"commonAncestorContainer", 6}}) {
    range_template->PrototypeTemplate()->SetAccessorProperty(
        v8::String::NewFromUtf8(isolate, property.first).ToLocalChecked(),
        v8::FunctionTemplate::New(
            isolate, RangePropertyFunctionGetter,
            v8::Integer::New(isolate, property.second)));
  }
  range_template->PrototypeTemplate()->Set(
      isolate, "setStart",
      v8::FunctionTemplate::New(isolate, RangeSetBoundary,
                                v8::True(isolate)));
  range_template->PrototypeTemplate()->Set(
      isolate, "setEnd",
      v8::FunctionTemplate::New(isolate, RangeSetBoundary,
                                v8::False(isolate)));
  range_template->PrototypeTemplate()->Set(
      isolate, "selectNode",
      v8::FunctionTemplate::New(isolate, RangeSelectNode));
  range_template->PrototypeTemplate()->Set(
      isolate, "selectNodeContents",
      v8::FunctionTemplate::New(isolate, RangeSelectNodeContents));
  range_template->PrototypeTemplate()->Set(
      isolate, "collapse",
      v8::FunctionTemplate::New(isolate, RangeCollapse));
  range_template->PrototypeTemplate()->Set(
      isolate, "cloneRange",
      v8::FunctionTemplate::New(isolate, RangeCloneRange));
  range_template->PrototypeTemplate()->Set(
      isolate, "cloneContents",
      v8::FunctionTemplate::New(isolate, RangeContents,
                                v8::Integer::New(isolate, 1)));
  range_template->PrototypeTemplate()->Set(
      isolate, "extractContents",
      v8::FunctionTemplate::New(isolate, RangeContents,
                                v8::Integer::New(isolate, 2)));
  range_template->PrototypeTemplate()->Set(
      isolate, "deleteContents",
      v8::FunctionTemplate::New(isolate, RangeContents,
                                v8::Integer::New(isolate, 3)));
  range_template->PrototypeTemplate()->Set(
      isolate, "insertNode",
      v8::FunctionTemplate::New(isolate, RangeInsertNode));
  range_template->PrototypeTemplate()->Set(
      isolate, "detach", v8::FunctionTemplate::New(isolate, RangeDetach));
  tree_walker_template->InstanceTemplate()->SetInternalFieldCount(1);
  node_iterator_template->InstanceTemplate()->SetInternalFieldCount(1);
  for (const auto &property :
       {std::pair<const char *, int>{"root", 1}, {"whatToShow", 3},
        {"filter", 4}}) {
    v8::Local<v8::String> name =
        v8::String::NewFromUtf8(isolate, property.first).ToLocalChecked();
    v8::Local<v8::FunctionTemplate> getter = v8::FunctionTemplate::New(
        isolate, TraversalPropertyFunctionGetter,
        v8::Integer::New(isolate, property.second));
    tree_walker_template->PrototypeTemplate()->SetAccessorProperty(name,
                                                                    getter);
    node_iterator_template->PrototypeTemplate()->SetAccessorProperty(name,
                                                                      getter);
  }
  tree_walker_template->PrototypeTemplate()->SetAccessorProperty(
      v8::String::NewFromUtf8Literal(isolate, "currentNode"),
      v8::FunctionTemplate::New(isolate, TraversalPropertyFunctionGetter,
                                v8::Integer::New(isolate, 2)),
      v8::FunctionTemplate::New(isolate, TreeWalkerCurrentNodeSetter));
  node_iterator_template->PrototypeTemplate()->SetAccessorProperty(
      v8::String::NewFromUtf8Literal(isolate, "referenceNode"),
      v8::FunctionTemplate::New(isolate, TraversalPropertyFunctionGetter,
                                v8::Integer::New(isolate, 2)));
  node_iterator_template->PrototypeTemplate()->SetAccessorProperty(
      v8::String::NewFromUtf8Literal(isolate, "pointerBeforeReferenceNode"),
      v8::FunctionTemplate::New(isolate, TraversalPropertyFunctionGetter,
                                v8::Integer::New(isolate, 6)));
  for (const auto &method :
       {std::pair<const char *, int>{"parentNode", 1}, {"firstChild", 2},
        {"lastChild", 3}, {"previousSibling", 4}, {"nextSibling", 5}}) {
    tree_walker_template->PrototypeTemplate()->Set(
        isolate, method.first,
        v8::FunctionTemplate::New(isolate, TreeWalkerRelation,
                                  v8::Integer::New(isolate, method.second)));
  }
  tree_walker_template->PrototypeTemplate()->Set(
      isolate, "previousNode",
      v8::FunctionTemplate::New(isolate, TraversalStep, v8::False(isolate)));
  tree_walker_template->PrototypeTemplate()->Set(
      isolate, "nextNode",
      v8::FunctionTemplate::New(isolate, TraversalStep, v8::True(isolate)));
  node_iterator_template->PrototypeTemplate()->Set(
      isolate, "previousNode",
      v8::FunctionTemplate::New(isolate, TraversalStep, v8::False(isolate)));
  node_iterator_template->PrototypeTemplate()->Set(
      isolate, "nextNode",
      v8::FunctionTemplate::New(isolate, TraversalStep, v8::True(isolate)));
  node_iterator_template->PrototypeTemplate()->Set(
      isolate, "detach",
      v8::FunctionTemplate::New(isolate, NodeIteratorDetach));
  selection_template->InstanceTemplate()->SetInternalFieldCount(1);
  for (const auto &property :
       {std::pair<const char *, int>{"anchorNode", 1}, {"anchorOffset", 2},
        {"focusNode", 3}, {"focusOffset", 4}, {"isCollapsed", 5},
        {"rangeCount", 6}, {"type", 7}}) {
    selection_template->PrototypeTemplate()->SetAccessorProperty(
        v8::String::NewFromUtf8(isolate, property.first).ToLocalChecked(),
        v8::FunctionTemplate::New(
            isolate, SelectionPropertyFunctionGetter,
            v8::Integer::New(isolate, property.second)));
  }
  selection_template->PrototypeTemplate()->Set(
      isolate, "getRangeAt",
      v8::FunctionTemplate::New(isolate, SelectionGetRangeAt));
  selection_template->PrototypeTemplate()->Set(
      isolate, "addRange",
      v8::FunctionTemplate::New(isolate, SelectionAddRange));
  selection_template->PrototypeTemplate()->Set(
      isolate, "removeAllRanges",
      v8::FunctionTemplate::New(isolate, SelectionRemoveAllRanges));
  selection_template->PrototypeTemplate()->Set(
      isolate, "empty",
      v8::FunctionTemplate::New(isolate, SelectionRemoveAllRanges));
  selection_template->PrototypeTemplate()->Set(
      isolate, "collapse", v8::FunctionTemplate::New(isolate, SelectionCollapse));
  selection_template->PrototypeTemplate()->Set(
      isolate, "setPosition",
      v8::FunctionTemplate::New(isolate, SelectionCollapse));
  selection_template->PrototypeTemplate()->Set(
      isolate, "collapseToStart",
      v8::FunctionTemplate::New(isolate, SelectionCollapseToEdge,
                                v8::True(isolate)));
  selection_template->PrototypeTemplate()->Set(
      isolate, "collapseToEnd",
      v8::FunctionTemplate::New(isolate, SelectionCollapseToEdge,
                                v8::False(isolate)));
  selection_template->PrototypeTemplate()->Set(
      isolate, "selectAllChildren",
      v8::FunctionTemplate::New(isolate, SelectionSelectAllChildren));
  selection_template->PrototypeTemplate()->Set(
      isolate, "deleteFromDocument",
      v8::FunctionTemplate::New(isolate, SelectionDeleteFromDocument));
  selection_template->PrototypeTemplate()->Set(
      isolate, "toString",
      v8::FunctionTemplate::New(isolate, SelectionToString));

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

  for (const char *name : {"length", "state"}) {
    history_template->PrototypeTemplate()->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        HistoryPropertyGetter);
  }
  history_template->PrototypeTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "scrollRestoration"),
      HistoryPropertyGetter, HistoryScrollRestorationSetter);
  history_template->PrototypeTemplate()->Set(
      isolate, "pushState",
      v8::FunctionTemplate::New(isolate, HistoryUpdateState,
                                v8::False(isolate)));
  history_template->PrototypeTemplate()->Set(
      isolate, "replaceState",
      v8::FunctionTemplate::New(isolate, HistoryUpdateState,
                                v8::True(isolate)));
  history_template->PrototypeTemplate()->Set(
      isolate, "go", v8::FunctionTemplate::New(isolate, HistoryTraverse));
  history_template->PrototypeTemplate()->Set(
      isolate, "back",
      v8::FunctionTemplate::New(isolate, HistoryTraverse,
                                v8::Integer::New(isolate, -1)));
  history_template->PrototypeTemplate()->Set(
      isolate, "forward",
      v8::FunctionTemplate::New(isolate, HistoryTraverse,
                                v8::Integer::New(isolate, 1)));
  history_template->PrototypeTemplate()->Set(
      v8::Symbol::GetToStringTag(isolate),
      v8::String::NewFromUtf8Literal(isolate, "History"),
      static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontEnum));

  for (const char *name : {"href", "protocol", "host", "hostname", "port",
                           "pathname", "search", "hash"}) {
    location_template->PrototypeTemplate()->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        LocationPropertyGetter, LocationPropertySetter);
  }
  location_template->PrototypeTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "origin"),
      LocationPropertyGetter);
  location_template->PrototypeTemplate()->Set(
      isolate, "assign",
      v8::FunctionTemplate::New(isolate, LocationNavigate,
                                v8::Integer::New(isolate, 1)));
  location_template->PrototypeTemplate()->Set(
      isolate, "replace",
      v8::FunctionTemplate::New(isolate, LocationNavigate,
                                v8::Integer::New(isolate, 2)));
  location_template->PrototypeTemplate()->Set(
      isolate, "reload",
      v8::FunctionTemplate::New(isolate, LocationNavigate,
                                v8::Integer::New(isolate, 3)));
  location_template->PrototypeTemplate()->Set(
      isolate, "toString",
      v8::FunctionTemplate::New(isolate, LocationToString));
  location_template->PrototypeTemplate()->Set(
      v8::Symbol::GetToStringTag(isolate),
      v8::String::NewFromUtf8Literal(isolate, "Location"),
      static_cast<v8::PropertyAttribute>(v8::ReadOnly | v8::DontEnum));

  auto install_event_surface =
      [isolate](v8::Local<v8::ObjectTemplate> object) {
        for (const char *name : {"type", "target", "currentTarget",
                                 "eventPhase", "bubbles", "cancelable",
                                 "composed", "defaultPrevented", "isTrusted",
                                 "timeStamp", "relatedTarget", "state",
                                 "oldURL", "newURL", "persisted"}) {
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
  composition_event_template->PrototypeTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "data"), EventPropertyGetter);
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {event_template, custom_event_template, mouse_event_template, pointer_event_template,
        keyboard_event_template, input_event_template,
        composition_event_template, focus_event_template}) {
    install_event_surface(interface_template->InstanceTemplate());
  }
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {mouse_event_template, pointer_event_template}) {
    install_mouse_event_surface(interface_template->InstanceTemplate());
  }
  install_pointer_event_surface(pointer_event_template->InstanceTemplate());
  install_keyboard_event_surface(keyboard_event_template->InstanceTemplate());
  install_input_event_surface(input_event_template->InstanceTemplate());
  composition_event_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "data"), EventPropertyGetter);
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
  node_prototype->Set(isolate, "cloneNode",
                      v8::FunctionTemplate::New(isolate, NodeCloneNode));
  node_prototype->Set(isolate, "normalize",
                      v8::FunctionTemplate::New(isolate, NodeNormalize));

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
        for (const auto &method :
             {std::pair<const char *, DOMMutationOperation>{
                  "append", DOMMutationOperation::Append},
              {"prepend", DOMMutationOperation::Prepend},
              {"replaceChildren", DOMMutationOperation::ReplaceChildren}}) {
          prototype->Set(
              isolate, method.first,
              v8::FunctionTemplate::New(
                  isolate, NodeConvenienceMutation,
                  v8::Integer::NewFromUnsigned(
                      isolate, static_cast<uint8_t>(method.second))));
        }
      };

  auto install_child_node_surface =
      [isolate](v8::Local<v8::ObjectTemplate> prototype) {
        for (const auto &method :
             {std::pair<const char *, DOMMutationOperation>{
                  "before", DOMMutationOperation::Before},
              {"after", DOMMutationOperation::After},
              {"replaceWith", DOMMutationOperation::ReplaceWith},
              {"remove", DOMMutationOperation::Remove}}) {
          prototype->Set(
              isolate, method.first,
              v8::FunctionTemplate::New(
                  isolate, NodeConvenienceMutation,
                  v8::Integer::NewFromUnsigned(
                      isolate, static_cast<uint8_t>(method.second))));
        }
      };

  v8::Local<v8::ObjectTemplate> element_prototype =
      element_template->PrototypeTemplate();
  install_parent_node_surface(element_prototype);
  install_child_node_surface(element_prototype);
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
  element_prototype->Set(
      isolate, "getBoundingClientRect",
      v8::FunctionTemplate::New(isolate, ElementGetBoundingClientRect));
  element_prototype->Set(
      isolate, "getClientRects",
      v8::FunctionTemplate::New(isolate, ElementGetClientRects));
  element_prototype->Set(
      isolate, "scroll",
      v8::FunctionTemplate::New(isolate, ElementScroll, v8::False(isolate)));
  element_prototype->Set(
      isolate, "scrollTo",
      v8::FunctionTemplate::New(isolate, ElementScroll, v8::False(isolate)));
  element_prototype->Set(
      isolate, "scrollBy",
      v8::FunctionTemplate::New(isolate, ElementScroll, v8::True(isolate)));
  element_prototype->Set(
      isolate, "scrollIntoView",
      v8::FunctionTemplate::New(isolate, ElementScrollIntoView));
  for (const char *name : {"clientWidth", "clientHeight", "offsetWidth",
                           "offsetHeight", "scrollWidth", "scrollHeight"}) {
    element_prototype->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        ElementGeometryGetter);
  }
  for (const char *name : {"scrollLeft", "scrollTop"}) {
    element_prototype->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        ElementGeometryGetter, ElementScrollSetter);
  }

  v8::Local<v8::ObjectTemplate> html_element_prototype =
      html_element_template->PrototypeTemplate();
  for (const char *name : {"title", "lang", "dir", "htmlFor"}) {
    html_element_prototype->SetNativeDataProperty(
        v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
        NodeReflectedAttributeGetter, NodeReflectedAttributeSetter);
  }
  html_element_prototype->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "hidden"),
      NodeReflectedBooleanGetter, NodeReflectedBooleanSetter);
  html_element_prototype->Set(
      isolate, "focus",
      v8::FunctionTemplate::New(isolate, HTMLElementFocus,
                                v8::True(isolate)));
  html_element_prototype->Set(
      isolate, "blur",
      v8::FunctionTemplate::New(isolate, HTMLElementFocus,
                                v8::False(isolate)));

  auto install_form_value_prototype =
      [isolate](v8::Local<v8::FunctionTemplate> interface_template) {
        interface_template->PrototypeTemplate()->SetAccessorProperty(
            v8::String::NewFromUtf8Literal(isolate, "value"),
            v8::FunctionTemplate::New(isolate,
                                      ElementFormValueFunctionGetter),
            v8::FunctionTemplate::New(isolate,
                                      ElementFormValueFunctionSetter));
      };
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {html_input_element_template, html_text_area_element_template,
        html_select_element_template, html_option_element_template,
        html_button_element_template}) {
    install_form_value_prototype(interface_template);
  }
  html_input_element_template->PrototypeTemplate()->SetAccessorProperty(
      v8::String::NewFromUtf8Literal(isolate, "checked"),
      v8::FunctionTemplate::New(isolate, ElementFormCheckedFunctionGetter),
      v8::FunctionTemplate::New(isolate,
                                ElementFormCheckedFunctionSetter));
  html_input_element_template->PrototypeTemplate()->SetAccessorProperty(
      v8::String::NewFromUtf8Literal(isolate, "indeterminate"),
      v8::FunctionTemplate::New(isolate,
                                ElementFormIndeterminateFunctionGetter),
      v8::FunctionTemplate::New(isolate,
                                ElementFormIndeterminateFunctionSetter));
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {html_input_element_template, html_text_area_element_template}) {
    for (const auto &property :
         {std::pair<const char *, int>{"selectionStart", 1},
          {"selectionEnd", 2}, {"selectionDirection", 3}}) {
      interface_template->PrototypeTemplate()->SetAccessorProperty(
          v8::String::NewFromUtf8(isolate, property.first).ToLocalChecked(),
          v8::FunctionTemplate::New(
              isolate, ElementFormSelectionFunctionGetter,
              v8::Integer::New(isolate, property.second)),
          v8::FunctionTemplate::New(
              isolate, ElementFormSelectionFunctionSetter,
              v8::Integer::New(isolate, property.second)));
    }
    interface_template->PrototypeTemplate()->Set(
        isolate, "setSelectionRange",
        v8::FunctionTemplate::New(isolate, ElementSetSelectionRange));
    interface_template->PrototypeTemplate()->Set(
        isolate, "select",
        v8::FunctionTemplate::New(isolate, ElementSelectText));
  }
  html_option_element_template->PrototypeTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "selected"),
      ElementFormSelectedGetter, ElementFormSelectedSetter);
  html_select_element_template->PrototypeTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "selectedIndex"),
      ElementFormSelectedIndexGetter, ElementFormSelectedIndexSetter);
  html_select_element_template->PrototypeTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "options"),
      ElementFormCollectionGetter, nullptr,
      facade_data(FacadeKind::SelectOptions), v8::DontEnum);
  html_form_element_template->PrototypeTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "elements"),
      ElementFormCollectionGetter, nullptr,
      facade_data(FacadeKind::FormElements), v8::DontEnum);
  html_form_element_template->PrototypeTemplate()->Set(
      isolate, "reset",
      v8::FunctionTemplate::New(isolate, HTMLFormElementReset));
  html_form_element_template->PrototypeTemplate()->Set(
      isolate, "checkValidity",
      v8::FunctionTemplate::New(isolate, HTMLFormElementCheckValidity));
  html_form_element_template->PrototypeTemplate()->Set(
      isolate, "reportValidity",
      v8::FunctionTemplate::New(isolate, HTMLFormElementCheckValidity));
  html_form_element_template->PrototypeTemplate()->Set(
      isolate, "requestSubmit",
      v8::FunctionTemplate::New(isolate, HTMLFormElementRequestSubmit));
  html_form_element_template->PrototypeTemplate()->Set(
      isolate, "submit",
      v8::FunctionTemplate::New(isolate, HTMLFormElementSubmit));
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {html_input_element_template, html_text_area_element_template,
        html_select_element_template, html_button_element_template}) {
    interface_template->PrototypeTemplate()->SetNativeDataProperty(
        v8::String::NewFromUtf8Literal(isolate, "form"),
        ElementFormOwnerGetter);
  }

  auto install_reflected_string =
      [isolate](v8::Local<v8::FunctionTemplate> interface_template,
                const char *name) {
        interface_template->PrototypeTemplate()->SetNativeDataProperty(
            v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
            NodeReflectedAttributeGetter, NodeReflectedAttributeSetter);
      };
  auto install_reflected_boolean =
      [isolate](v8::Local<v8::FunctionTemplate> interface_template,
                const char *name) {
        interface_template->PrototypeTemplate()->SetNativeDataProperty(
            v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
            NodeReflectedBooleanGetter, NodeReflectedBooleanSetter);
      };
  for (const char *name : {"defaultValue", "name", "type", "placeholder"})
    install_reflected_string(html_input_element_template, name);
  for (const char *name : {"defaultChecked", "disabled", "multiple",
                           "required", "readOnly"})
    install_reflected_boolean(html_input_element_template, name);
  for (const char *name : {"defaultValue", "name", "placeholder"})
    install_reflected_string(html_text_area_element_template, name);
  for (const char *name : {"disabled", "required", "readOnly"})
    install_reflected_boolean(html_text_area_element_template, name);
  install_reflected_string(html_select_element_template, "name");
  for (const char *name : {"disabled", "multiple", "required"})
    install_reflected_boolean(html_select_element_template, name);
  for (const char *name : {"defaultSelected", "disabled"})
    install_reflected_boolean(html_option_element_template, name);
  for (const char *name : {"name", "type"})
    install_reflected_string(html_button_element_template, name);
  install_reflected_boolean(html_button_element_template, "disabled");
  for (const char *name : {"name", "action", "method"})
    install_reflected_string(html_form_element_template, name);
  for (const char *name : {"name", "src"})
    install_reflected_string(html_iframe_element_template, name);

  install_child_node_surface(text_template->PrototypeTemplate());
  text_template->PrototypeTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "data"), NodeValueGetter,
      NodeValueSetter);
  text_template->PrototypeTemplate()->Set(
      isolate, "splitText",
      v8::FunctionTemplate::New(isolate, TextSplitText));
  html_template_element_template->PrototypeTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "content"),
      HTMLTemplateElementContentGetter);

  v8::Local<v8::ObjectTemplate> document_prototype =
      document_template->PrototypeTemplate();
  install_parent_node_surface(document_prototype);
  for (const char *name : {"documentElement", "scrollingElement", "head", "body"}) {
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
      isolate, "getElementsByTagName",
      v8::FunctionTemplate::New(isolate, DocumentGetElementsByTagName));
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
  document_prototype->Set(
      isolate, "importNode",
      v8::FunctionTemplate::New(isolate, DocumentImportOrAdoptNode,
                                v8::False(isolate)));
  document_prototype->Set(
      isolate, "adoptNode",
      v8::FunctionTemplate::New(isolate, DocumentImportOrAdoptNode,
                                v8::True(isolate)));
  document_prototype->Set(
      isolate, "createRange",
      v8::FunctionTemplate::New(isolate, DocumentCreateRange));
  document_prototype->Set(
      isolate, "createTreeWalker",
      v8::FunctionTemplate::New(isolate, DocumentCreateTraversal,
                                v8::False(isolate)));
  document_prototype->Set(
      isolate, "createNodeIterator",
      v8::FunctionTemplate::New(isolate, DocumentCreateTraversal,
                                v8::True(isolate)));
  document_prototype->Set(
      isolate, "getSelection", v8::FunctionTemplate::New(isolate, GetSelection));

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
        for (const char *name : {"clientWidth", "clientHeight", "offsetWidth",
                                 "offsetHeight", "scrollWidth",
                                 "scrollHeight"}) {
          instance->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              ElementGeometryGetter);
        }
        for (const char *name : {"scrollLeft", "scrollTop"}) {
          instance->SetNativeDataProperty(
              v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
              ElementGeometryGetter, ElementScrollSetter);
        }
      };
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {node_template, element_template, html_element_template, text_template,
        document_template, document_fragment_template,
        html_form_element_template, html_input_element_template,
        html_text_area_element_template, html_select_element_template,
        html_option_element_template, html_button_element_template,
        html_template_element_template, html_iframe_element_template,
        html_head_element_template, html_script_element_template,
        html_media_element_template, html_image_element_template}) {
    install_node_instance_surface(interface_template->InstanceTemplate());
  }
  install_element_instance_surface(element_template->InstanceTemplate());
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {html_element_template, html_form_element_template,
        html_input_element_template, html_text_area_element_template,
        html_select_element_template, html_option_element_template,
        html_button_element_template, html_template_element_template,
        html_iframe_element_template, html_head_element_template,
        html_script_element_template, html_media_element_template,
        html_image_element_template}) {
    install_element_instance_surface(interface_template->InstanceTemplate());
    for (const char *name : {"title", "lang", "dir", "htmlFor"}) {
      interface_template->InstanceTemplate()->SetNativeDataProperty(
          v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
          NodeReflectedAttributeGetter, NodeReflectedAttributeSetter);
    }
    interface_template->InstanceTemplate()->SetNativeDataProperty(
        v8::String::NewFromUtf8Literal(isolate, "hidden"),
        NodeReflectedBooleanGetter, NodeReflectedBooleanSetter);
  }

  auto install_form_value_instance =
      [isolate](v8::Local<v8::FunctionTemplate> interface_template) {
        interface_template->InstanceTemplate()->SetNativeDataProperty(
            v8::String::NewFromUtf8Literal(isolate, "value"),
            ElementFormValueGetter, ElementFormValueSetter);
      };
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {html_input_element_template, html_text_area_element_template,
        html_select_element_template, html_option_element_template,
        html_button_element_template}) {
    install_form_value_instance(interface_template);
  }
  html_input_element_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "checked"),
      ElementFormCheckedGetter, ElementFormCheckedSetter);
  html_input_element_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "indeterminate"),
      ElementFormIndeterminateGetter, ElementFormIndeterminateSetter);
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {html_input_element_template, html_text_area_element_template}) {
    for (const char *name : {"selectionStart", "selectionEnd",
                             "selectionDirection"}) {
      interface_template->InstanceTemplate()->SetNativeDataProperty(
          v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
          ElementFormSelectionGetter, ElementFormSelectionSetter);
    }
  }
  html_option_element_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "selected"),
      ElementFormSelectedGetter, ElementFormSelectedSetter);
  html_select_element_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "selectedIndex"),
      ElementFormSelectedIndexGetter, ElementFormSelectedIndexSetter);
  html_select_element_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "options"),
      ElementFormCollectionGetter, nullptr,
      facade_data(FacadeKind::SelectOptions), v8::DontEnum);
  html_form_element_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "elements"),
      ElementFormCollectionGetter, nullptr,
      facade_data(FacadeKind::FormElements), v8::DontEnum);
  for (v8::Local<v8::FunctionTemplate> interface_template :
       {html_input_element_template, html_text_area_element_template,
        html_select_element_template, html_button_element_template}) {
    interface_template->InstanceTemplate()->SetNativeDataProperty(
        v8::String::NewFromUtf8Literal(isolate, "form"),
        ElementFormOwnerGetter);
  }
  html_template_element_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "content"),
      HTMLTemplateElementContentGetter);

  auto install_instance_reflected_string =
      [isolate](v8::Local<v8::FunctionTemplate> interface_template,
                const char *name) {
        interface_template->InstanceTemplate()->SetNativeDataProperty(
            v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
            NodeReflectedAttributeGetter, NodeReflectedAttributeSetter);
      };
  auto install_instance_reflected_boolean =
      [isolate](v8::Local<v8::FunctionTemplate> interface_template,
                const char *name) {
        interface_template->InstanceTemplate()->SetNativeDataProperty(
            v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
            NodeReflectedBooleanGetter, NodeReflectedBooleanSetter);
      };
  for (const char *name : {"defaultValue", "name", "type", "placeholder"})
    install_instance_reflected_string(html_input_element_template, name);
  for (const char *name : {"defaultChecked", "disabled", "multiple",
                           "required", "readOnly"})
    install_instance_reflected_boolean(html_input_element_template, name);
  for (const char *name : {"defaultValue", "name", "placeholder"})
    install_instance_reflected_string(html_text_area_element_template, name);
  for (const char *name : {"disabled", "required", "readOnly"})
    install_instance_reflected_boolean(html_text_area_element_template, name);
  install_instance_reflected_string(html_select_element_template, "name");
  for (const char *name : {"disabled", "multiple", "required"})
    install_instance_reflected_boolean(html_select_element_template, name);
  for (const char *name : {"defaultSelected", "disabled"})
    install_instance_reflected_boolean(html_option_element_template, name);
  for (const char *name : {"name", "type"})
    install_instance_reflected_string(html_button_element_template, name);
  install_instance_reflected_boolean(html_button_element_template,
                                     "disabled");
  for (const char *name : {"name", "action", "method"})
    install_instance_reflected_string(html_form_element_template, name);
  for (const char *name : {"name", "src"})
    install_instance_reflected_string(html_iframe_element_template, name);
  text_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "data"), NodeValueGetter,
      NodeValueSetter);
  install_parent_instance_surface(document_template->InstanceTemplate());
  install_parent_instance_surface(
      document_fragment_template->InstanceTemplate());
  for (const char *name : {"documentElement", "scrollingElement", "head", "body"}) {
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
  document_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "readyState"),
      DocumentReadyStateGetter);
  document_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "title"), DocumentTitleGetter,
      DocumentTitleSetter);
  document_template->InstanceTemplate()->SetNativeDataProperty(
      v8::String::NewFromUtf8Literal(isolate, "location"),
      DocumentLocationGetter);

  realm->event_target_template.Reset(isolate, event_target_template);
  realm->node_template.Reset(isolate, node_template);
  realm->element_template.Reset(isolate, element_template);
  realm->html_element_template.Reset(isolate, html_element_template);
  realm->html_form_element_template.Reset(isolate, html_form_element_template);
  realm->form_data_template.Reset(isolate, form_data_template);
  realm->html_input_element_template.Reset(isolate,
                                            html_input_element_template);
  realm->html_text_area_element_template.Reset(
      isolate, html_text_area_element_template);
  realm->html_select_element_template.Reset(isolate,
                                             html_select_element_template);
  realm->html_option_element_template.Reset(isolate,
                                             html_option_element_template);
  realm->html_button_element_template.Reset(isolate,
                                             html_button_element_template);
  realm->html_template_element_template.Reset(
      isolate, html_template_element_template);
  realm->html_iframe_element_template.Reset(isolate,
                                             html_iframe_element_template);
  realm->html_head_element_template.Reset(isolate, html_head_element_template);
  realm->html_script_element_template.Reset(isolate,
                                             html_script_element_template);
  realm->html_media_element_template.Reset(isolate,
                                            html_media_element_template);
  realm->html_image_element_template.Reset(isolate,
                                            html_image_element_template);
  realm->text_template.Reset(isolate, text_template);
  realm->document_template.Reset(isolate, document_template);
  realm->document_fragment_template.Reset(isolate,
                                           document_fragment_template);
  realm->history_template.Reset(isolate, history_template);
  realm->location_template.Reset(isolate, location_template);
  realm->event_template.Reset(isolate, event_template);
  realm->custom_event_template.Reset(isolate, custom_event_template);
  realm->mouse_event_template.Reset(isolate, mouse_event_template);
  realm->pointer_event_template.Reset(isolate, pointer_event_template);
  realm->keyboard_event_template.Reset(isolate, keyboard_event_template);
  realm->input_event_template.Reset(isolate, input_event_template);
  realm->composition_event_template.Reset(isolate,
                                            composition_event_template);
  realm->focus_event_template.Reset(isolate, focus_event_template);
  realm->mutation_observer_template.Reset(isolate,
                                           mutation_observer_template);
  realm->mutation_record_template.Reset(isolate, mutation_record_template);
  realm->range_template.Reset(isolate, range_template);
  realm->tree_walker_template.Reset(isolate, tree_walker_template);
  realm->node_iterator_template.Reset(isolate, node_iterator_template);
  realm->selection_template.Reset(isolate, selection_template);
  realm->dom_rect_template.Reset(isolate, dom_rect_template);

  v8::Local<v8::ObjectTemplate> style_prototype =
      style_template->PrototypeTemplate();
  v8::Local<v8::ObjectTemplate> style_instance =
      style_template->InstanceTemplate();
  for (v8::Local<v8::ObjectTemplate> surface : {style_prototype,
                                                 style_instance}) {
    surface->SetNativeDataProperty(
        v8::String::NewFromUtf8Literal(isolate, "cssText"), StyleCSSTextGetter,
        StyleCSSTextSetter);
    surface->SetNativeDataProperty(
        v8::String::NewFromUtf8Literal(isolate, "length"), StyleLengthGetter);
  }
  style_prototype->Set(isolate, "item",
                       v8::FunctionTemplate::New(isolate, StyleItem));
  style_prototype->Set(
      isolate, "getPropertyValue",
      v8::FunctionTemplate::New(isolate, StyleGetPropertyValue));
  style_prototype->Set(
      isolate, "getPropertyPriority",
      v8::FunctionTemplate::New(isolate, StyleGetPropertyPriority));
  style_prototype->Set(isolate, "setProperty",
                       v8::FunctionTemplate::New(isolate, StyleSetProperty));
  style_prototype->Set(
      isolate, "removeProperty",
      v8::FunctionTemplate::New(isolate, StyleRemoveProperty));
  for (const char *name : {
           "display",          "direction",          "color",              "content",
           "background",
           "backgroundColor",  "boxSizing",          "fontFamily",
           "fontSize",         "fontStyle",           "fontWeight",
           "lineHeight",       "textDecoration",     "textDecorationLine",
           "textAlign",        "verticalAlign",       "opacity",
           "width",
           "height",           "minHeight",          "minWidth",
           "maxHeight",        "maxWidth",
           "overflow",         "overflowX",          "overflowY",
           "position",         "top",                "right",
           "bottom",           "left",               "zIndex",
           "alignContent",     "alignItems",          "alignSelf",
           "columnGap",        "flex",
           "flexBasis",        "flexDirection",       "flexGrow",
           "flexShrink",       "gap",                 "justifyContent",
           "justifyItems",     "justifySelf",         "order",
           "rowGap",           "gridArea",            "gridAutoColumns",
           "gridAutoFlow",     "gridAutoRows",        "gridColumn",
           "gridColumnEnd",    "gridColumnStart",     "gridRow",
           "gridRowEnd",       "gridRowStart",        "gridTemplateAreas",
           "gridTemplateColumns", "gridTemplateRows",
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
           "borderCollapse",   "borderSpacing",     "captionSide",
           "emptyCells",       "tableLayout",        "listStyle",
           "listStyleType",    "visibility",         "whiteSpace",
           "textOrientation", "writingMode",
           "cssFloat"}) {
    for (v8::Local<v8::ObjectTemplate> surface : {style_prototype,
                                                   style_instance}) {
      surface->SetNativeDataProperty(
          v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
          StyleDirectPropertyGetter, StyleDirectPropertySetter);
    }
  }
  style_instance->SetHandler(v8::IndexedPropertyHandlerConfiguration(
      StyleIndexedGetter, StyleIndexedSetter, StyleIndexedQuery, nullptr,
      StyleIndexedEnumerator));
  style_instance->SetHandler(v8::NamedPropertyHandlerConfiguration(
      StyleNamedGetter, StyleNamedSetter, StyleNamedQuery, nullptr, nullptr,
      v8::Local<v8::Value>(), v8::PropertyHandlerFlags::kOnlyInterceptStrings));
  realm->style_template.Reset(isolate, style_template);
  realm->resize_observer_template.Reset(isolate, resize_observer_template);
  realm->intersection_observer_template.Reset(
      isolate, intersection_observer_template);

  v8::Local<v8::Function> queue_microtask;
  v8::Local<v8::Function> set_timeout;
  v8::Local<v8::Function> clear_timeout;
  v8::Local<v8::Function> request_animation_frame;
  v8::Local<v8::Function> cancel_animation_frame;
  v8::Local<v8::Function> fetch_host;
  v8::Local<v8::Function> storage_host;
  v8::Local<v8::Function> websocket_host;
  v8::Local<v8::Function> get_computed_style;
  v8::Local<v8::Function> get_selection;
  v8::Local<v8::Function> window_scroll;
  v8::Local<v8::Function> window_scroll_by;
  if (!v8::Function::New(context, QueueMicrotaskCallback)
           .ToLocal(&queue_microtask) ||
      !v8::Function::New(context, SetTimeoutCallback).ToLocal(&set_timeout) ||
      !v8::Function::New(context, ClearTimeoutCallback)
           .ToLocal(&clear_timeout) ||
      !v8::Function::New(context, RequestAnimationFrameCallback)
           .ToLocal(&request_animation_frame) ||
      !v8::Function::New(context, CancelAnimationFrameCallback)
           .ToLocal(&cancel_animation_frame) ||
      !v8::Function::New(context, FetchHostCallback).ToLocal(&fetch_host) ||
      !v8::Function::New(context, StorageHostCallback).ToLocal(&storage_host) ||
      !v8::Function::New(context, WebSocketHostCallback)
           .ToLocal(&websocket_host) ||
      !v8::Function::New(context, GetComputedStyle)
           .ToLocal(&get_computed_style) ||
      !v8::Function::New(context, GetSelection).ToLocal(&get_selection)) {
    return false;
  }
  if (!v8::Function::New(context, WindowScroll, v8::False(isolate))
           .ToLocal(&window_scroll) ||
      !v8::Function::New(context, WindowScroll, v8::True(isolate))
           .ToLocal(&window_scroll_by)) {
    return false;
  }
  v8::Local<v8::Object> global = context->Global();
  v8::Local<v8::Object> history_object;
  v8::Local<v8::Object> location_object;
  v8::Local<v8::Function> window_add_event_listener;
  v8::Local<v8::Function> window_remove_event_listener;
  v8::Local<v8::Function> window_dispatch_event;
  if (!history_template->InstanceTemplate()
           ->NewInstance(context)
           .ToLocal(&history_object) ||
      !location_template->InstanceTemplate()
           ->NewInstance(context)
           .ToLocal(&location_object) ||
      !v8::Function::New(context, WindowAddEventListener)
           .ToLocal(&window_add_event_listener) ||
      !v8::Function::New(context, WindowRemoveEventListener)
           .ToLocal(&window_remove_event_listener) ||
      !v8::Function::New(context, WindowDispatchEvent)
           .ToLocal(&window_dispatch_event) ||
      !global
           ->DefineOwnProperty(
               context, v8::String::NewFromUtf8Literal(isolate, "history"),
               history_object,
               static_cast<v8::PropertyAttribute>(v8::ReadOnly |
                                                  v8::DontDelete))
           .FromMaybe(false) ||
      !global
           ->SetNativeDataProperty(
               context, v8::String::NewFromUtf8Literal(isolate, "location"),
               GlobalLocationGetter, GlobalLocationSetter,
               v8::Local<v8::Value>(), v8::DontDelete)
           .FromMaybe(false) ||
      !global
           ->Set(context,
                 v8::String::NewFromUtf8Literal(isolate, "addEventListener"),
                 window_add_event_listener)
           .FromMaybe(false) ||
      !global
           ->Set(context,
                 v8::String::NewFromUtf8Literal(isolate,
                                                "removeEventListener"),
                 window_remove_event_listener)
           .FromMaybe(false) ||
      !global
           ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "dispatchEvent"),
                   window_dispatch_event)
           .FromMaybe(false) ||
      !global
           ->Set(context,
                 v8::String::NewFromUtf8Literal(isolate,
                                                "__gossamerHostFetch"),
                 fetch_host)
           .FromMaybe(false) ||
      !global
           ->Set(context,
                 v8::String::NewFromUtf8Literal(isolate,
                                                "__gossamerHostStorage"),
                 storage_host)
           .FromMaybe(false) ||
      !global
           ->Set(context,
                 v8::String::NewFromUtf8Literal(isolate,
                                                "__gossamerHostWebSocket"),
                 websocket_host)
           .FromMaybe(false)) {
    return false;
  }
  realm->history_object.Reset(isolate, history_object);
  realm->location_object.Reset(isolate, location_object);
  v8::Local<v8::Object> performance = v8::Object::New(isolate);
  v8::Local<v8::Function> performance_now;
  if (!v8::Function::New(context, PerformanceNowCallback)
           .ToLocal(&performance_now) ||
      !performance
           ->Set(context, v8::String::NewFromUtf8Literal(isolate, "now"),
                 performance_now)
           .FromMaybe(false)) {
    return false;
  }
  for (const char *name : {"innerWidth", "innerHeight", "scrollX", "scrollY",
                           "pageXOffset", "pageYOffset"}) {
    if (!global
             ->SetNativeDataProperty(
                 context,
                 v8::String::NewFromUtf8(isolate, name).ToLocalChecked(),
                 GlobalViewportGetter, nullptr, v8::Local<v8::Value>(),
                 v8::DontEnum)
             .FromMaybe(false)) {
      return false;
    }
  }
  v8::Local<v8::Object> node_filter = v8::Object::New(isolate);
  for (const auto &constant :
       {std::pair<const char *, uint32_t>{"FILTER_ACCEPT", 1},
        {"FILTER_REJECT", 2},
        {"FILTER_SKIP", 3},
        {"SHOW_ALL", 0xffffffffu},
        {"SHOW_ELEMENT", 0x1},
        {"SHOW_ATTRIBUTE", 0x2},
        {"SHOW_TEXT", 0x4},
        {"SHOW_CDATA_SECTION", 0x8},
        {"SHOW_ENTITY_REFERENCE", 0x10},
        {"SHOW_ENTITY", 0x20},
        {"SHOW_PROCESSING_INSTRUCTION", 0x40},
        {"SHOW_COMMENT", 0x80},
        {"SHOW_DOCUMENT", 0x100},
        {"SHOW_DOCUMENT_TYPE", 0x200},
        {"SHOW_DOCUMENT_FRAGMENT", 0x400},
        {"SHOW_NOTATION", 0x800}}) {
    if (!node_filter
             ->DefineOwnProperty(
                 context,
                 v8::String::NewFromUtf8(isolate, constant.first)
                     .ToLocalChecked(),
                 v8::Integer::NewFromUnsigned(isolate, constant.second),
                 static_cast<v8::PropertyAttribute>(v8::ReadOnly |
                                                    v8::DontDelete))
             .FromMaybe(false)) {
      return false;
    }
  }
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
  if (!(expose_interface("DOMException", dom_exception_template) &&
         expose_interface("EventTarget", event_target_template) &&
         expose_interface("Event", event_template) &&
         expose_interface("CustomEvent", custom_event_template) &&
         expose_interface("MouseEvent", mouse_event_template) &&
         expose_interface("PointerEvent", pointer_event_template) &&
         expose_interface("KeyboardEvent", keyboard_event_template) &&
         expose_interface("InputEvent", input_event_template) &&
         expose_interface("CompositionEvent", composition_event_template) &&
         expose_interface("FocusEvent", focus_event_template) &&
         expose_interface("MutationObserver", mutation_observer_template) &&
         expose_interface("MutationRecord", mutation_record_template) &&
         expose_interface("ResizeObserver", resize_observer_template) &&
         expose_interface("IntersectionObserver",
                          intersection_observer_template) &&
         expose_interface("Range", range_template) &&
         expose_interface("TreeWalker", tree_walker_template) &&
         expose_interface("NodeIterator", node_iterator_template) &&
         expose_interface("Selection", selection_template) &&
         expose_interface("Node", node_template) &&
         expose_interface("Element", element_template) &&
         expose_interface("HTMLElement", html_element_template) &&
         expose_interface("HTMLFormElement", html_form_element_template) &&
         expose_interface("FormData", form_data_template) &&
         expose_interface("HTMLInputElement", html_input_element_template) &&
         expose_interface("HTMLTextAreaElement",
                          html_text_area_element_template) &&
         expose_interface("HTMLSelectElement", html_select_element_template) &&
         expose_interface("HTMLOptionElement", html_option_element_template) &&
         expose_interface("HTMLButtonElement", html_button_element_template) &&
         expose_interface("HTMLTemplateElement",
                          html_template_element_template) &&
         expose_interface("HTMLIFrameElement", html_iframe_element_template) &&
         expose_interface("HTMLHeadElement", html_head_element_template) &&
         expose_interface("HTMLScriptElement", html_script_element_template) &&
         expose_interface("HTMLMediaElement", html_media_element_template) &&
         expose_interface("HTMLImageElement", html_image_element_template) &&
         expose_interface("Text", text_template) &&
         expose_interface("Document", document_template) &&
         expose_interface("DocumentFragment", document_fragment_template) &&
         expose_interface("History", history_template) &&
         expose_interface("Location", location_template) &&
         expose_interface("NodeList", node_list_template) &&
         expose_interface("HTMLCollection", html_collection_template) &&
         expose_interface("DOMTokenList", token_list_template) &&
         expose_interface("DOMStringMap", dataset_template) &&
         expose_interface("CSSStyleDeclaration", style_template) &&
         expose_interface("DOMRect", dom_rect_template) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "NodeFilter"),
                   node_filter)
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
             .FromMaybe(false) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate,
                                                  "requestAnimationFrame"),
                   request_animation_frame)
             .FromMaybe(false) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate,
                                                  "cancelAnimationFrame"),
                   cancel_animation_frame)
             .FromMaybe(false) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "performance"),
                   performance)
             .FromMaybe(false) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate,
                                                  "getComputedStyle"),
                   get_computed_style)
             .FromMaybe(false) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "getSelection"),
                   get_selection)
             .FromMaybe(false) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "scroll"),
                   window_scroll)
             .FromMaybe(false) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "scrollTo"),
                   window_scroll)
             .FromMaybe(false) &&
         global
             ->Set(context,
                   v8::String::NewFromUtf8Literal(isolate, "scrollBy"),
                   window_scroll_by)
             .FromMaybe(false))) {
    return false;
  }
  return InstallURLSearchParams(context);
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
  case 19:
    state->type = "compositionstart";
    state->interface = EventInterface::CompositionEvent;
    state->bubbles = state->composed = true;
    break;
  case 20:
    state->type = "compositionupdate";
    state->interface = EventInterface::CompositionEvent;
    state->bubbles = state->composed = true;
    break;
  case 21:
    state->type = "compositionend";
    state->interface = EventInterface::CompositionEvent;
    state->bubbles = state->composed = true;
    break;
  case 22:
    state->type = "submit";
    state->interface = EventInterface::Event;
    state->bubbles = true;
    state->cancelable = true;
    break;
  case 23:
    state->type = "invalid";
    state->interface = EventInterface::Event;
    state->cancelable = true;
    break;
  case 24:
	state->type = "scroll";
	state->interface = EventInterface::Event;
	break;
  case 25:
	state->type = "resize";
	state->interface = EventInterface::Event;
	break;
  case 26:
    state->type = "DOMContentLoaded";
    state->interface = EventInterface::Event;
    break;
  case 27:
    state->type = "load";
    state->interface = EventInterface::Event;
    break;
  case 28:
    state->type = "popstate";
    state->interface = EventInterface::Event;
    break;
  case 29:
    state->type = "hashchange";
    state->interface = EventInterface::Event;
    break;
  case 30:
    state->type = "beforeunload";
    state->interface = EventInterface::Event;
    state->cancelable = true;
    break;
  case 31:
    state->type = "pagehide";
    state->interface = EventInterface::Event;
    break;
  case 32:
    state->type = "unload";
    state->interface = EventInterface::Event;
    break;
  case 33:
    state->type = "pageshow";
    state->interface = EventInterface::Event;
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
  state->persisted = input->persisted != 0;
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
  for (MutationObserverWeakData *observer : realm->mutation_observers) {
    if (observer->object.IsWeak())
      observer->object.ClearWeak<MutationObserverWeakData>();
    observer->object.Reset();
    if (observer->state != nullptr) {
      observer->state->callback.Reset();
      observer->state->registrations.clear();
      delete observer->state;
    }
    delete observer;
  }
  realm->mutation_observers.clear();
  for (LayoutObserverWeakData *observer : realm->layout_observers) {
    if (observer->object.IsWeak())
      observer->object.ClearWeak<LayoutObserverWeakData>();
    observer->object.Reset();
    if (observer->state != nullptr) {
      observer->state->callback.Reset();
      observer->state->registrations.clear();
      delete observer->state;
    }
    delete observer;
  }
  realm->layout_observers.clear();
  if (realm->selection_state != nullptr) {
    realm->selection_state->range_object.Reset();
    delete realm->selection_state;
    realm->selection_state = nullptr;
  }
  realm->selection_object.Reset();
  for (RangeWeakData *range : realm->ranges) {
    if (range->object.IsWeak())
      range->object.ClearWeak<RangeWeakData>();
    range->object.Reset();
    if (range->state != nullptr) {
      range->state->start_object.Reset();
      range->state->end_object.Reset();
      delete range->state;
    }
    delete range;
  }
  realm->ranges.clear();
  for (TraversalWeakData *traversal : realm->traversals) {
    if (traversal->object.IsWeak())
      traversal->object.ClearWeak<TraversalWeakData>();
    traversal->object.Reset();
    if (traversal->state != nullptr) {
      traversal->state->root_object.Reset();
      traversal->state->filter.Reset();
      delete traversal->state;
    }
    delete traversal;
  }
  realm->traversals.clear();
  for (auto &callback : realm->callbacks)
    callback.second.Reset();
  realm->callbacks.clear();
  realm->timer_callbacks.clear();
  realm->callback_timers.clear();
  realm->animation_frame_callbacks.clear();
  realm->callback_animation_frames.clear();
  for (auto &module : realm->modules)
    module.second.Reset();
  realm->modules.clear();
  realm->module_resolutions.clear();
  realm->event_target_template.Reset();
  realm->event_template.Reset();
  realm->custom_event_template.Reset();
  realm->mouse_event_template.Reset();
  realm->pointer_event_template.Reset();
  realm->keyboard_event_template.Reset();
  realm->input_event_template.Reset();
  realm->composition_event_template.Reset();
  realm->focus_event_template.Reset();
  realm->mutation_observer_template.Reset();
  realm->mutation_record_template.Reset();
  realm->resize_observer_template.Reset();
  realm->intersection_observer_template.Reset();
  realm->range_template.Reset();
  realm->tree_walker_template.Reset();
  realm->node_iterator_template.Reset();
  realm->selection_template.Reset();
  realm->dom_rect_template.Reset();
  realm->node_list_template.Reset();
  realm->html_collection_template.Reset();
  realm->token_list_template.Reset();
  realm->dataset_template.Reset();
  realm->node_template.Reset();
  realm->element_template.Reset();
  realm->html_element_template.Reset();
  realm->html_form_element_template.Reset();
  realm->form_data_template.Reset();
  realm->html_input_element_template.Reset();
  realm->html_text_area_element_template.Reset();
  realm->html_select_element_template.Reset();
  realm->html_option_element_template.Reset();
  realm->html_button_element_template.Reset();
  realm->html_template_element_template.Reset();
  realm->html_iframe_element_template.Reset();
  realm->html_head_element_template.Reset();
  realm->html_script_element_template.Reset();
  realm->html_media_element_template.Reset();
  realm->html_image_element_template.Reset();
  realm->text_template.Reset();
  realm->document_template.Reset();
  realm->document_fragment_template.Reset();
  realm->history_template.Reset();
  realm->location_template.Reset();
  realm->style_template.Reset();
  realm->collection_iterator_template.Reset();
  realm->document_wrapper.Reset();
  realm->history_object.Reset();
  realm->location_object.Reset();
  realm->document_bound = false;
  realm->document_key = WrapperKey{};
  realm->base_uri.clear();
  realm->scroll_restoration = "auto";
}

std::string ResolveURLReference(const std::string &base,
                                const std::string &reference) {
  size_t scheme = reference.find(':');
  size_t first_delimiter = reference.find_first_of("/?#");
  if (scheme != std::string::npos &&
      (first_delimiter == std::string::npos || scheme < first_delimiter))
    return reference;
  size_t authority = base.find("://");
  if (authority == std::string::npos)
    return reference;
  size_t origin_end = base.find('/', authority + 3);
  std::string origin =
      origin_end == std::string::npos ? base : base.substr(0, origin_end);
  if (reference.rfind("//", 0) == 0)
    return base.substr(0, authority + 1) + reference;
  if (!reference.empty() && reference[0] == '/')
    return origin + reference;

  std::string base_path = origin_end == std::string::npos
                              ? "/"
                              : base.substr(origin_end);
  size_t suffix = base_path.find_first_of("?#");
  if (suffix != std::string::npos)
    base_path.resize(suffix);
  if (!reference.empty() && (reference[0] == '?' || reference[0] == '#'))
    return origin + base_path + reference;
  size_t slash = base_path.rfind('/');
  std::string combined =
      (slash == std::string::npos ? "/" : base_path.substr(0, slash + 1)) +
      reference;
  std::vector<std::string> segments;
  size_t start = 0;
  while (start <= combined.size()) {
    size_t end = combined.find('/', start);
    std::string segment = combined.substr(
        start, end == std::string::npos ? std::string::npos : end - start);
    if (segment == "..") {
      if (!segments.empty())
        segments.pop_back();
    } else if (!segment.empty() && segment != ".") {
      segments.push_back(segment);
    }
    if (end == std::string::npos)
      break;
    start = end + 1;
  }
  std::string resolved = origin + "/";
  for (size_t index = 0; index < segments.size(); ++index) {
    if (index != 0)
      resolved.push_back('/');
    resolved += segments[index];
  }
  return resolved;
}

void ImportMetaResolve(const v8::FunctionCallbackInfo<v8::Value> &info) {
  v8::Isolate *isolate = info.GetIsolate();
  std::string base = UTF8Value(isolate, info.Data());
  std::string reference;
  if (!StringFromValue(isolate,
                       info.Length() == 0 ? v8::Undefined(isolate) : info[0],
                       &reference))
    return;
  std::string resolved = ResolveURLReference(base, reference);
  v8::Local<v8::String> value;
  if (!NewUTF8String(isolate, resolved.data(), resolved.size(), &value)) {
    ThrowError(isolate, "V8 failed to allocate resolved module URL");
    return;
  }
  info.GetReturnValue().Set(value);
}

void InitializeImportMetaObject(v8::Local<v8::Context> context,
                                v8::Local<v8::Module> module,
                                v8::Local<v8::Object> meta) {
  v8::Isolate *isolate = v8::Isolate::GetCurrent();
  v8::Local<v8::Value> resource_name = module->GetResourceName();
  if (resource_name.IsEmpty())
    resource_name = v8::String::Empty(isolate);
  meta->CreateDataProperty(
      context, v8::String::NewFromUtf8Literal(isolate, "url"), resource_name)
      .FromMaybe(false);
  v8::Local<v8::Function> resolve;
  if (v8::Function::New(context, ImportMetaResolve, resource_name)
          .ToLocal(&resolve)) {
    meta->CreateDataProperty(
        context, v8::String::NewFromUtf8Literal(isolate, "resolve"), resolve)
        .FromMaybe(false);
  }
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
    realm->isolate->SetHostInitializeImportMetaObjectCallback(
        InitializeImportMetaObject);
    realm->isolate->SetHostImportModuleWithPhaseDynamicallyCallback(
        ImportModuleDynamically);
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

extern "C" int gossamer_v8_realm_evaluate_module(
    gossamer_v8_realm *realm, const gossamer_v8_host *host,
    const char *root_url, size_t root_url_length,
    const gossamer_v8_module_source *sources, size_t source_count,
    const gossamer_v8_module_resolution *resolutions, size_t resolution_count,
    char **error_out) {
  if (!RequireRealm(realm, error_out))
    return 0;
  if (root_url == nullptr || root_url_length == 0 || sources == nullptr ||
      source_count == 0) {
    SetError(error_out, "V8 module graph has no root or sources");
    return 0;
  }
  auto valid_string = [](const char *data, size_t length) {
    return (data != nullptr || length == 0) &&
           length <= static_cast<size_t>(std::numeric_limits<int>::max());
  };
  if (!valid_string(root_url, root_url_length)) {
    SetError(error_out, "V8 module root URL exceeds the supported length");
    return 0;
  }
  for (size_t index = 0; index < source_count; ++index) {
    if (!valid_string(sources[index].url, sources[index].url_length) ||
        !valid_string(sources[index].source, sources[index].source_length)) {
      SetError(error_out,
               "V8 module source or URL exceeds the supported length");
      return 0;
    }
  }
  for (size_t index = 0; index < resolution_count; ++index) {
    if (!valid_string(resolutions[index].referrer,
                      resolutions[index].referrer_length) ||
        !valid_string(resolutions[index].specifier,
                      resolutions[index].specifier_length) ||
        !valid_string(resolutions[index].url, resolutions[index].url_length)) {
      SetError(error_out, "V8 module resolution exceeds the supported length");
      return 0;
    }
  }

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
    realm->evaluation_nanos.fetch_add(MonotonicNanos() - started,
                                      std::memory_order_relaxed);
    SetError(error_out, binding_error);
    return 0;
  }
  v8::TryCatch caught(realm->isolate);

  for (size_t index = 0; index < resolution_count; ++index) {
    const auto &edge = resolutions[index];
    std::string referrer(edge.referrer == nullptr ? "" : edge.referrer,
                         edge.referrer_length);
    std::string specifier(edge.specifier == nullptr ? "" : edge.specifier,
                          edge.specifier_length);
    std::string target(edge.url == nullptr ? "" : edge.url, edge.url_length);
    realm->module_resolutions[ModuleResolutionKey(referrer, specifier)] =
        std::move(target);
  }

  for (size_t index = 0; index < source_count; ++index) {
    const auto &entry = sources[index];
    std::string url(entry.url == nullptr ? "" : entry.url, entry.url_length);
    if (realm->modules.find(url) != realm->modules.end())
      continue;
    v8::Local<v8::String> source_string;
    v8::Local<v8::String> url_string;
    if (!v8::String::NewFromUtf8(
             realm->isolate, entry.source == nullptr ? "" : entry.source,
             v8::NewStringType::kNormal,
             static_cast<int>(entry.source_length))
             .ToLocal(&source_string) ||
        !v8::String::NewFromUtf8(realm->isolate, url.data(),
                                 v8::NewStringType::kNormal,
                                 static_cast<int>(url.size()))
             .ToLocal(&url_string)) {
      realm->evaluation_nanos.fetch_add(MonotonicNanos() - started,
                                        std::memory_order_relaxed);
      SetError(error_out, "V8 failed to allocate a module source or URL");
      return 0;
    }
    v8::ScriptOrigin origin(url_string, 0, 0, false, -1,
                            v8::Local<v8::Value>(), false, false, true);
    v8::ScriptCompiler::Source compiler_source(source_string, origin);
    v8::Local<v8::Module> module;
    if (!v8::ScriptCompiler::CompileModule(realm->isolate, &compiler_source)
             .ToLocal(&module)) {
      realm->evaluation_nanos.fetch_add(MonotonicNanos() - started,
                                        std::memory_order_relaxed);
      SetError(error_out, DescribeException(realm->isolate, context, caught));
      return 0;
    }
    realm->modules.emplace(url,
                           v8::Global<v8::Module>(realm->isolate, module));
  }

  std::string root(root_url, root_url_length);
  auto root_entry = realm->modules.find(root);
  if (root_entry == realm->modules.end()) {
    realm->evaluation_nanos.fetch_add(MonotonicNanos() - started,
                                      std::memory_order_relaxed);
    SetError(error_out, "V8 module root source was not supplied");
    return 0;
  }
  v8::Local<v8::Module> module = root_entry->second.Get(realm->isolate);
  bool ok = true;
  if (module->GetStatus() == v8::Module::kUninstantiated)
    ok = module->InstantiateModule(context, ResolveModule).FromMaybe(false);
  if (ok && module->GetStatus() == v8::Module::kInstantiated) {
    v8::Local<v8::Value> result;
    ok = module->Evaluate(context).ToLocal(&result);
  }
  if (ok && module->GetStatus() == v8::Module::kErrored) {
    realm->isolate->ThrowException(module->GetException());
    ok = false;
  }
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

extern "C" int gossamer_v8_realm_dispatch_websocket(
    gossamer_v8_realm *realm, const gossamer_v8_host *host,
    const char *event_json, size_t event_json_length, char **error_out) {
  if (!RequireRealm(realm, error_out))
    return 0;
  if (event_json == nullptr) {
    SetError(error_out, "V8 received a null WebSocket event");
    return 0;
  }
  std::lock_guard<std::mutex> guard(realm->mutex);
  if (!RequireRealm(realm, error_out))
    return 0;

  v8::Locker locker(realm->isolate);
  v8::Isolate::Scope isolate_scope(realm->isolate);
  v8::HandleScope handle_scope(realm->isolate);
  v8::Local<v8::Context> context = realm->context.Get(realm->isolate);
  v8::Context::Scope context_scope(context);
  HostScope host_scope(realm, host);
  v8::TryCatch caught(realm->isolate);
  v8::Local<v8::Value> dispatcher_value;
  if (!context->Global()
           ->Get(context, v8::String::NewFromUtf8Literal(
                              realm->isolate,
                              "__gossamerDispatchWebSocket"))
           .ToLocal(&dispatcher_value) ||
      !dispatcher_value->IsFunction()) {
    SetError(error_out, "V8 WebSocket dispatcher is unavailable");
    return 0;
  }
  v8::Local<v8::String> event_value;
  if (!v8::String::NewFromUtf8(realm->isolate, event_json,
                               v8::NewStringType::kNormal,
                               static_cast<int>(event_json_length))
           .ToLocal(&event_value)) {
    SetError(error_out, "V8 failed to allocate a WebSocket event");
    return 0;
  }
  v8::Local<v8::Value> result;
  v8::Local<v8::Value> arguments[] = {event_value};
  if (!dispatcher_value.As<v8::Function>()
           ->Call(context, context->Global(), 1, arguments)
           .ToLocal(&result)) {
    SetError(error_out, DescribeException(realm->isolate, context, caught));
    return 0;
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

extern "C" int gossamer_v8_realm_invoke_animation_frame(
    gossamer_v8_realm *realm, const gossamer_v8_host *host,
    uint64_t callback, double timestamp_milliseconds, char **error_out) {
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
    SetError(error_out, "V8 animation callback handle is unknown or canceled");
    return 0;
  }
  v8::Local<v8::Function> function = found->second.Get(realm->isolate);
  RemoveCallback(realm, callback);
  realm->callbacks_invoked.fetch_add(1, std::memory_order_relaxed);

  v8::TryCatch caught(realm->isolate);
  v8::Local<v8::Value> argument =
      v8::Number::New(realm->isolate, timestamp_milliseconds);
  v8::Local<v8::Value> result;
  if (!function->Call(context, context->Global(), 1, &argument)
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
  std::string observer_error;
  if (!DeliverMutationObservers(realm, context, &observer_error)) {
    SetError(error_out, observer_error);
    return 0;
  }
  realm->isolate->PerformMicrotaskCheckpoint();
  if (!DeliverLayoutObservers(realm, context, &observer_error)) {
    SetError(error_out, observer_error);
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

extern "C" int
gossamer_v8_realm_bfcache_eligible(gossamer_v8_realm *realm) {
  if (realm == nullptr || realm->closed)
    return 0;
  std::lock_guard<std::mutex> guard(realm->mutex);
  if (realm->closed)
    return 0;
  for (const auto &entry : realm->listeners) {
    if (entry.first.type != "beforeunload" && entry.first.type != "unload")
      continue;
    for (const auto &listener : entry.second) {
      if (!listener->removed)
        return 0;
    }
  }
  return realm->timer_callbacks.empty() &&
         realm->animation_frame_callbacks.empty();
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
