#ifndef GOSSAMER_INTERNAL_V8ENGINE_BRIDGE_H_
#define GOSSAMER_INTERNAL_V8ENGINE_BRIDGE_H_

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#if defined(__GNUC__)
#define GOSSAMER_V8_EXPORT __attribute__((visibility("default")))
#else
#define GOSSAMER_V8_EXPORT
#endif

typedef struct gossamer_v8_realm gossamer_v8_realm;

typedef struct gossamer_v8_module_source {
  const char *url;
  size_t url_length;
  const char *source;
  size_t source_length;
} gossamer_v8_module_source;

typedef struct gossamer_v8_module_resolution {
  const char *referrer;
  size_t referrer_length;
  const char *specifier;
  size_t specifier_length;
  const char *url;
  size_t url_length;
} gossamer_v8_module_resolution;

typedef struct gossamer_v8_mutation_record {
  uint64_t sequence;
  uint8_t type;
  uint32_t target;
  uint32_t *added_nodes;
  size_t added_count;
  uint32_t *removed_nodes;
  size_t removed_count;
  uint32_t previous_sibling;
  int has_previous_sibling;
  uint32_t next_sibling;
  int has_next_sibling;
  char *attribute_name;
  size_t attribute_name_length;
  char *old_value;
  size_t old_value_length;
  int old_value_present;
} gossamer_v8_mutation_record;

typedef struct gossamer_v8_rect {
  double x;
  double y;
  double width;
  double height;
} gossamer_v8_rect;

typedef struct gossamer_v8_element_geometry {
  gossamer_v8_rect rect;
  double client_width;
  double client_height;
  double offset_width;
  double offset_height;
  double scroll_width;
  double scroll_height;
  double scroll_left;
  double scroll_top;
} gossamer_v8_element_geometry;

typedef struct gossamer_v8_viewport_geometry {
  double inner_width;
  double inner_height;
  double scroll_x;
  double scroll_y;
  double scroll_width;
  double scroll_height;
} gossamer_v8_viewport_geometry;

// gossamer_v8_host is valid only for one engine entry. It contains numeric
// execution identity and C callbacks; no Go pointer is stored in V8.
typedef struct gossamer_v8_host {
  uint64_t execution_id;
  int (*document_metadata)(uint64_t execution_id, uint64_t *document_out,
                           uint32_t *node_out, char **base_uri_out,
                           size_t *base_uri_length_out, int *found_out,
                           char **error_out);
  int (*document_ready_state)(uint64_t execution_id, char **value_out,
                              size_t *value_length_out, char **error_out);
  int (*get_element_by_id)(uint64_t execution_id, const char *value,
                           size_t value_length, uint64_t *document_out,
                           uint32_t *node_out, int *found_out,
                           char **error_out);
  int (*create_element)(uint64_t execution_id, const char *name,
                        size_t name_length, uint64_t *document_out,
                        uint32_t *node_out, char **error_out);
  int (*create_element_ns)(uint64_t execution_id, const char *namespace_uri,
                           size_t namespace_uri_length,
                           const char *qualified_name,
                           size_t qualified_name_length,
                           uint64_t *document_out, uint32_t *node_out,
                           char **error_out);
  int (*create_text_node)(uint64_t execution_id, const char *data,
                          size_t data_length, uint64_t *document_out,
                          uint32_t *node_out, char **error_out);
  int (*create_document_fragment)(uint64_t execution_id,
                                  uint64_t *document_out,
                                  uint32_t *node_out, char **error_out);
  int (*text_content)(uint64_t execution_id, uint64_t document, uint32_t node,
                      char **value_out, size_t *value_length_out,
                      char **error_out);
  int (*set_text_content)(uint64_t execution_id, uint64_t document,
                          uint32_t node, const char *value, size_t value_length,
                          char **error_out);
  int (*append_child)(uint64_t execution_id, uint64_t parent_document,
                      uint32_t parent_node, uint64_t child_document,
                      uint32_t child_node, char **error_out);
  int (*insert_before)(uint64_t execution_id, uint64_t parent_document,
                       uint32_t parent_node, uint64_t child_document,
                       uint32_t child_node, uint64_t reference_document,
                       uint32_t reference_node, char **error_out);
  int (*remove_child)(uint64_t execution_id, uint64_t parent_document,
                      uint32_t parent_node, uint64_t child_document,
                      uint32_t child_node, char **error_out);
  int (*get_attribute)(uint64_t execution_id, uint64_t document, uint32_t node,
                       const char *name, size_t name_length, char **value_out,
                       size_t *value_length_out, int *found_out,
                       char **error_out);
  int (*set_attribute)(uint64_t execution_id, uint64_t document, uint32_t node,
                       const char *name, size_t name_length, const char *value,
                       size_t value_length, char **error_out);
  int (*remove_attribute)(uint64_t execution_id, uint64_t document,
                          uint32_t node, const char *name, size_t name_length,
                          char **error_out);
  int (*node_metadata)(uint64_t execution_id, uint64_t document, uint32_t node,
                       uint8_t *type_out, char **node_name_out,
                       size_t *node_name_length_out, char **local_name_out,
                       size_t *local_name_length_out, char **namespace_uri_out,
                       size_t *namespace_uri_length_out, char **prefix_out,
                       size_t *prefix_length_out, int *connected_out,
                       char **error_out);
  int (*related_node)(uint64_t execution_id, uint64_t document, uint32_t node,
                      uint8_t relation, uint32_t *related_node_out,
                      int *found_out, char **error_out);
  int (*child_nodes)(uint64_t execution_id, uint64_t document, uint32_t node,
                     int elements_only, uint32_t **nodes_out,
                     size_t *count_out, char **error_out);
  int (*contains)(uint64_t execution_id, uint64_t document, uint32_t node,
                  uint64_t other_document, uint32_t other_node,
                  int *contains_out, char **error_out);
  int (*replace_child)(uint64_t execution_id, uint64_t parent_document,
                       uint32_t parent_node, uint64_t child_document,
                       uint32_t child_node, uint64_t replaced_document,
                       uint32_t replaced_node, char **error_out);
  int (*mutate_nodes)(uint64_t execution_id, uint64_t receiver_document,
                      uint32_t receiver_node, uint8_t operation,
                      const uint64_t *documents, const uint32_t *nodes,
                      size_t count, char **error_out);
  int (*node_value)(uint64_t execution_id, uint64_t document, uint32_t node,
                    char **value_out, size_t *value_length_out,
                    int *non_null_out, char **error_out);
  int (*set_node_value)(uint64_t execution_id, uint64_t document,
                        uint32_t node, const char *value, size_t value_length,
                        char **error_out);
  int (*has_attribute)(uint64_t execution_id, uint64_t document, uint32_t node,
                       const char *name, size_t name_length, int *found_out,
                       char **error_out);
  int (*attribute_count)(uint64_t execution_id, uint64_t document,
                         uint32_t node, size_t *count_out, char **error_out);
  int (*attribute_name)(uint64_t execution_id, uint64_t document,
                        uint32_t node, size_t index, char **name_out,
                        size_t *name_length_out, int *found_out,
                        char **error_out);
  int (*query_selector)(uint64_t execution_id, uint64_t document,
                        uint32_t node, const char *selector,
                        size_t selector_length, int all,
                        uint32_t **nodes_out, size_t *count_out,
                        char **error_out);
  int (*matches_selector)(uint64_t execution_id, uint64_t document,
                          uint32_t node, const char *selector,
                          size_t selector_length, int *matches_out,
                          char **error_out);
  int (*closest_selector)(uint64_t execution_id, uint64_t document,
                          uint32_t node, const char *selector,
                          size_t selector_length, uint32_t *closest_node_out,
                          int *found_out, char **error_out);
  int (*clone_node)(uint64_t execution_id, uint64_t document, uint32_t node,
                    int deep, uint64_t *clone_document_out,
                    uint32_t *clone_node_out, char **error_out);
  int (*template_content)(uint64_t execution_id, uint64_t document,
                          uint32_t node, uint64_t *content_document_out,
                          uint32_t *content_node_out, char **error_out);
  int (*split_text)(uint64_t execution_id, uint64_t document, uint32_t node,
                    int32_t offset, uint64_t *split_document_out,
                    uint32_t *split_node_out, char **error_out);
  int (*normalize_node)(uint64_t execution_id, uint64_t document,
                        uint32_t node, char **error_out);
  int (*adopt_node)(uint64_t execution_id, uint64_t document, uint32_t node,
                    uint64_t *adopted_document_out,
                    uint32_t *adopted_node_out, char **error_out);
  int (*range_contents)(uint64_t execution_id, uint64_t start_document,
                        uint32_t start_node, int32_t start_offset,
                        uint64_t end_document, uint32_t end_node,
                        int32_t end_offset, uint8_t operation,
                        uint64_t *fragment_document_out,
                        uint32_t *fragment_node_out, char **error_out);
  int (*inner_html)(uint64_t execution_id, uint64_t document, uint32_t node,
                    char **value_out, size_t *value_length_out,
                    char **error_out);
  int (*set_inner_html)(uint64_t execution_id, uint64_t document,
                        uint32_t node, const char *value, size_t value_length,
                        char **error_out);
  int (*insert_adjacent_html)(uint64_t execution_id, uint64_t document,
                              uint32_t node, const char *position,
                              size_t position_length, const char *value,
                              size_t value_length, char **error_out);
  int (*form_value)(uint64_t execution_id, uint64_t document, uint32_t node,
                    char **value_out, size_t *value_length_out,
                    char **error_out);
  int (*set_form_value)(uint64_t execution_id, uint64_t document,
                        uint32_t node, const char *value,
                        size_t value_length, char **error_out);
  int (*form_selection)(uint64_t execution_id, uint64_t document,
                        uint32_t node, int32_t *start_out, int32_t *end_out,
                        char **direction_out, size_t *direction_length_out,
                        char **error_out);
  int (*set_form_selection)(uint64_t execution_id, uint64_t document,
                            uint32_t node, int32_t start, int32_t end,
                            const char *direction, size_t direction_length,
                            char **error_out);
  int (*form_checked)(uint64_t execution_id, uint64_t document, uint32_t node,
                      int *checked_out, char **error_out);
  int (*set_form_checked)(uint64_t execution_id, uint64_t document,
                          uint32_t node, int checked, char **error_out);
  int (*form_selected)(uint64_t execution_id, uint64_t document,
                       uint32_t node, int *selected_out, char **error_out);
  int (*set_form_selected)(uint64_t execution_id, uint64_t document,
                           uint32_t node, int selected, char **error_out);
  int (*form_selected_index)(uint64_t execution_id, uint64_t document,
                             uint32_t node, int32_t *index_out,
                             char **error_out);
  int (*set_form_selected_index)(uint64_t execution_id, uint64_t document,
                                 uint32_t node, int32_t index,
                                 char **error_out);
  int (*form_control_nodes)(uint64_t execution_id, uint64_t document,
                            uint32_t node, uint8_t kind,
                            uint32_t **nodes_out, size_t *count_out,
                            char **error_out);
  int (*form_owner)(uint64_t execution_id, uint64_t document, uint32_t node,
                    uint32_t *owner_node_out, int *found_out,
                    char **error_out);
  int (*reset_form)(uint64_t execution_id, uint64_t document, uint32_t node,
                    char **error_out);
  int (*focus_node)(uint64_t execution_id, uint64_t document, uint32_t node,
                    int focused, char **error_out);
  int (*active_element)(uint64_t execution_id, uint64_t *document_out,
                        uint32_t *node_out, int *found_out,
                        char **error_out);
  int (*mutation_sequence)(uint64_t execution_id, uint64_t *sequence_out,
                           char **error_out);
  int (*mutation_records)(uint64_t execution_id, uint64_t since_sequence,
                          gossamer_v8_mutation_record **records_out,
                          size_t *count_out, uint64_t *latest_sequence_out,
                          char **error_out);
  int (*style_css_text)(uint64_t execution_id, uint64_t document,
                        uint32_t node, char **value_out,
                        size_t *value_length_out, char **error_out);
  int (*set_style_css_text)(uint64_t execution_id, uint64_t document,
                            uint32_t node, const char *value,
                            size_t value_length, char **error_out);
  int (*style_property)(uint64_t execution_id, uint64_t document,
                        uint32_t node, const char *name, size_t name_length,
                        char **value_out, size_t *value_length_out,
                        char **priority_out, size_t *priority_length_out,
                        int *found_out, char **error_out);
  int (*set_style_property)(uint64_t execution_id, uint64_t document,
                            uint32_t node, const char *name,
                            size_t name_length, const char *value,
                            size_t value_length, const char *priority,
                            size_t priority_length, char **error_out);
  int (*remove_style_property)(uint64_t execution_id, uint64_t document,
                               uint32_t node, const char *name,
                               size_t name_length, char **value_out,
                               size_t *value_length_out, char **error_out);
  int (*style_property_count)(uint64_t execution_id, uint64_t document,
                              uint32_t node, size_t *count_out,
                              char **error_out);
  int (*style_property_name)(uint64_t execution_id, uint64_t document,
                             uint32_t node, size_t index, char **name_out,
                             size_t *name_length_out, int *found_out,
                             char **error_out);
  int (*retain_node_wrapper)(uint64_t execution_id, uint64_t document,
                             uint32_t node, char **error_out);
  int (*retain_node_event_target)(uint64_t execution_id, uint64_t document,
                                  uint32_t node, char **error_out);
  int (*release_node_event_target)(uint64_t execution_id, uint64_t document,
                                   uint32_t node, char **error_out);
  int (*queue_callback)(uint64_t execution_id, uint64_t callback,
                        char **error_out);
  int (*queue_microtask)(uint64_t execution_id, uint64_t callback,
                         char **error_out);
  int (*set_timeout)(uint64_t execution_id, uint64_t callback,
                     int64_t delay_milliseconds, uint64_t *timer_out,
                     char **error_out);
  int (*clear_timeout)(uint64_t execution_id, uint64_t timer, char **error_out);
  int (*computed_style_property)(
      uint64_t execution_id, uint64_t document, uint32_t node,
      const char *pseudo, size_t pseudo_length, const char *name,
      size_t name_length, char **value_out, size_t *value_length_out,
      int *found_out, char **error_out);
  int (*computed_style_property_count)(
      uint64_t execution_id, uint64_t document, uint32_t node,
      const char *pseudo, size_t pseudo_length, size_t *count_out,
      char **error_out);
  int (*computed_style_property_name)(
      uint64_t execution_id, uint64_t document, uint32_t node,
      const char *pseudo, size_t pseudo_length, size_t index, char **name_out,
      size_t *name_length_out, int *found_out, char **error_out);
  // Keep bridge extensions append-only so independently rebuilt Go and C++
  // sides preserve the offsets of every existing callback.
  int (*form_validity)(uint64_t execution_id, uint64_t document,
                       uint32_t node, int *valid_out,
                       uint32_t **invalid_nodes_out, size_t *count_out,
                       char **error_out);
  int (*form_data_json)(uint64_t execution_id, uint64_t document,
                        uint32_t node, uint64_t submitter_document,
                        uint32_t submitter_node, char **json_out,
                        size_t *json_length_out, char **error_out);
  int (*submit_form)(uint64_t execution_id, uint64_t document, uint32_t node,
                     uint64_t submitter_document, uint32_t submitter_node,
                     char **error_out);
  int (*element_geometry)(uint64_t execution_id, uint64_t document,
                          uint32_t node,
                          gossamer_v8_element_geometry *geometry_out,
                          char **error_out);
  int (*viewport_geometry)(uint64_t execution_id,
                           gossamer_v8_viewport_geometry *geometry_out,
                           char **error_out);
  int (*scroll_element)(uint64_t execution_id, uint64_t document,
                        uint32_t node, double x, double y, int *changed_out,
                        char **error_out);
  int (*scroll_viewport)(uint64_t execution_id, double x, double y,
                         int *changed_out, char **error_out);
  int (*scroll_into_view)(uint64_t execution_id, uint64_t document,
                          uint32_t node, int *changed_out, char **error_out);
  int (*request_animation_frame)(uint64_t execution_id, uint64_t callback,
                                 uint64_t *frame_out, char **error_out);
  int (*cancel_animation_frame)(uint64_t execution_id, uint64_t frame,
                                char **error_out);
  int (*performance_now)(uint64_t execution_id, double *milliseconds_out,
                         char **error_out);
  int (*form_indeterminate)(uint64_t execution_id, uint64_t document,
                            uint32_t node, int *indeterminate_out,
                            char **error_out);
  int (*set_form_indeterminate)(uint64_t execution_id, uint64_t document,
                                uint32_t node, int indeterminate,
                                char **error_out);
} gossamer_v8_host;

typedef struct gossamer_v8_node_handle {
  uint64_t document;
  uint32_t node;
} gossamer_v8_node_handle;

typedef struct gossamer_v8_input_event {
  uint8_t type;
  uint64_t document;
  uint32_t node;
  uint64_t related_document;
  uint32_t related_node;
  double x;
  double y;
  int32_t button;
  uint32_t buttons;
  int32_t pointer_id;
  const char *pointer_type;
  size_t pointer_type_length;
  const char *key;
  size_t key_length;
  const char *code;
  size_t code_length;
  const char *data;
  size_t data_length;
  const char *input_type;
  size_t input_type_length;
  int is_primary;
  int repeat;
  int is_composing;
  int alt_key;
  int ctrl_key;
  int meta_key;
  int shift_key;
} gossamer_v8_input_event;

typedef struct gossamer_v8_heap_statistics {
  uint64_t total_heap_size;
  uint64_t total_heap_executable;
  uint64_t total_physical_size;
  uint64_t total_available_size;
  uint64_t used_heap_size;
  uint64_t heap_size_limit;
  uint64_t malloced_memory;
  uint64_t external_memory;
  uint64_t peak_malloced_memory;
  uint64_t native_contexts;
  uint64_t detached_contexts;
  uint64_t global_handles_size;
  uint64_t used_global_handles_size;
  uint64_t total_allocated_bytes;
} gossamer_v8_heap_statistics;

typedef struct gossamer_v8_profile {
  gossamer_v8_heap_statistics heap;
  uint64_t samples;
  uint64_t live_samples;
  uint64_t sampled_bytes;
  uint64_t live_bytes;
  uint64_t evaluations;
  uint64_t evaluation_nanos;
  uint64_t microtask_checkpoints;
  uint64_t foreground_tasks;
  uint64_t gc_prologues;
  uint64_t gc_epilogues;
  uint64_t minor_gcs;
  uint64_t major_gcs;
  uint64_t gc_nanos;
  uint64_t wrappers_created;
  uint64_t wrapper_cache_hits;
  uint64_t wrappers_collected;
  uint64_t live_wrappers;
  uint64_t callbacks_created;
  uint64_t callbacks_invoked;
  uint64_t live_callbacks;
  uint64_t event_listeners;
  uint64_t events_dispatched;
} gossamer_v8_profile;

GOSSAMER_V8_EXPORT int gossamer_v8_initialize(const char *icu_data_path,
                                              char **error_out);
GOSSAMER_V8_EXPORT const char *gossamer_v8_version(void);

GOSSAMER_V8_EXPORT gossamer_v8_realm *
gossamer_v8_realm_new(uint64_t sampling_interval, char **error_out);
GOSSAMER_V8_EXPORT int
gossamer_v8_realm_evaluate(gossamer_v8_realm *realm,
                           const gossamer_v8_host *host, const char *source,
                           size_t source_length, const char *source_url,
                           size_t source_url_length, char **error_out);
GOSSAMER_V8_EXPORT int gossamer_v8_realm_evaluate_module(
    gossamer_v8_realm *realm, const gossamer_v8_host *host,
    const char *root_url, size_t root_url_length,
    const gossamer_v8_module_source *sources, size_t source_count,
    const gossamer_v8_module_resolution *resolutions,
    size_t resolution_count, char **error_out);
GOSSAMER_V8_EXPORT int gossamer_v8_realm_dispatch_event(
    gossamer_v8_realm *realm, const gossamer_v8_host *host,
    const gossamer_v8_input_event *event, int *default_prevented_out,
    char **error_out);
GOSSAMER_V8_EXPORT int gossamer_v8_realm_invoke(gossamer_v8_realm *realm,
                                                const gossamer_v8_host *host,
                                                uint64_t callback,
                                                char **error_out);
GOSSAMER_V8_EXPORT int gossamer_v8_realm_invoke_animation_frame(
    gossamer_v8_realm *realm, const gossamer_v8_host *host, uint64_t callback,
    double timestamp_milliseconds, char **error_out);
GOSSAMER_V8_EXPORT int gossamer_v8_realm_drain_microtasks(
    gossamer_v8_realm *realm, const gossamer_v8_host *host, char **error_out);
GOSSAMER_V8_EXPORT int
gossamer_v8_realm_collect_garbage(gossamer_v8_realm *realm, char **error_out);
GOSSAMER_V8_EXPORT size_t gossamer_v8_realm_take_collected_wrappers(
    gossamer_v8_realm *realm, gossamer_v8_node_handle *handles_out,
    size_t capacity);
GOSSAMER_V8_EXPORT int
gossamer_v8_realm_profile(gossamer_v8_realm *realm,
                          gossamer_v8_profile *profile_out, char **error_out);
GOSSAMER_V8_EXPORT void gossamer_v8_realm_delete(gossamer_v8_realm *realm);

GOSSAMER_V8_EXPORT void gossamer_v8_error_free(char *error);

#undef GOSSAMER_V8_EXPORT

#ifdef __cplusplus
}
#endif

#endif // GOSSAMER_INTERNAL_V8ENGINE_BRIDGE_H_
