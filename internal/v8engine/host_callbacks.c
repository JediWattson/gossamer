//go:build v8 && cgo && darwin && arm64

#include "host_callbacks.h"

extern int goGossamerV8HostDocumentMetadata(
    uint64_t execution_id, uint64_t *document_out, uint32_t *node_out,
    char **base_uri_out, size_t *base_uri_length_out, int *found_out,
    char **error_out);

extern int goGossamerV8HostGetElementByID(uint64_t execution_id,
                                          const char *value,
                                          size_t value_length,
                                          uint64_t *document_out,
                                          uint32_t *node_out, int *found_out,
                                          char **error_out);
extern int goGossamerV8HostCreateElement(uint64_t execution_id,
                                         const char *name, size_t name_length,
                                         uint64_t *document_out,
                                         uint32_t *node_out, char **error_out);
extern int goGossamerV8HostCreateElementNS(
    uint64_t execution_id, const char *namespace_uri,
    size_t namespace_uri_length, const char *qualified_name,
    size_t qualified_name_length, uint64_t *document_out, uint32_t *node_out,
    char **error_out);
extern int goGossamerV8HostCreateTextNode(uint64_t execution_id,
                                          const char *data, size_t data_length,
                                          uint64_t *document_out,
                                          uint32_t *node_out, char **error_out);
extern int goGossamerV8HostTextContent(uint64_t execution_id, uint64_t document,
                                       uint32_t node, char **value_out,
                                       size_t *value_length_out,
                                       char **error_out);
extern int goGossamerV8HostSetTextContent(uint64_t execution_id,
                                          uint64_t document, uint32_t node,
                                          const char *value,
                                          size_t value_length,
                                          char **error_out);
extern int goGossamerV8HostAppendChild(uint64_t execution_id,
                                       uint64_t parent_document,
                                       uint32_t parent_node,
                                       uint64_t child_document,
                                       uint32_t child_node, char **error_out);
extern int
goGossamerV8HostInsertBefore(uint64_t execution_id, uint64_t parent_document,
                             uint32_t parent_node, uint64_t child_document,
                             uint32_t child_node, uint64_t reference_document,
                             uint32_t reference_node, char **error_out);
extern int goGossamerV8HostRemoveChild(uint64_t execution_id,
                                       uint64_t parent_document,
                                       uint32_t parent_node,
                                       uint64_t child_document,
                                       uint32_t child_node, char **error_out);
extern int goGossamerV8HostGetAttribute(uint64_t execution_id,
                                        uint64_t document, uint32_t node,
                                        const char *name, size_t name_length,
                                        char **value_out,
                                        size_t *value_length_out,
                                        int *found_out, char **error_out);
extern int goGossamerV8HostSetAttribute(uint64_t execution_id,
                                        uint64_t document, uint32_t node,
                                        const char *name, size_t name_length,
                                        const char *value, size_t value_length,
                                        char **error_out);
extern int goGossamerV8HostRemoveAttribute(uint64_t execution_id,
                                           uint64_t document, uint32_t node,
                                           const char *name, size_t name_length,
                                           char **error_out);
extern int goGossamerV8HostNodeMetadata(
    uint64_t execution_id, uint64_t document, uint32_t node,
    uint8_t *type_out, char **node_name_out, size_t *node_name_length_out,
    char **local_name_out, size_t *local_name_length_out,
    char **namespace_uri_out, size_t *namespace_uri_length_out,
    char **prefix_out, size_t *prefix_length_out, int *connected_out,
    char **error_out);
extern int goGossamerV8HostRelatedNode(uint64_t execution_id,
                                       uint64_t document, uint32_t node,
                                       uint8_t relation,
                                       uint32_t *related_node_out,
                                       int *found_out, char **error_out);
extern int goGossamerV8HostChildNodes(uint64_t execution_id,
                                      uint64_t document, uint32_t node,
                                      int elements_only, uint32_t **nodes_out,
                                      size_t *count_out, char **error_out);
extern int goGossamerV8HostContains(uint64_t execution_id, uint64_t document,
                                    uint32_t node, uint64_t other_document,
                                    uint32_t other_node, int *contains_out,
                                    char **error_out);
extern int goGossamerV8HostReplaceChild(
    uint64_t execution_id, uint64_t parent_document, uint32_t parent_node,
    uint64_t child_document, uint32_t child_node, uint64_t replaced_document,
    uint32_t replaced_node, char **error_out);
extern int goGossamerV8HostNodeValue(uint64_t execution_id, uint64_t document,
                                     uint32_t node, char **value_out,
                                     size_t *value_length_out,
                                     int *non_null_out, char **error_out);
extern int goGossamerV8HostSetNodeValue(uint64_t execution_id,
                                        uint64_t document, uint32_t node,
                                        const char *value,
                                        size_t value_length, char **error_out);
extern int goGossamerV8HostHasAttribute(uint64_t execution_id,
                                        uint64_t document, uint32_t node,
                                        const char *name, size_t name_length,
                                        int *found_out, char **error_out);
extern int goGossamerV8HostStyleCSSText(uint64_t execution_id,
                                        uint64_t document, uint32_t node,
                                        char **value_out,
                                        size_t *value_length_out,
                                        char **error_out);
extern int goGossamerV8HostSetStyleCSSText(uint64_t execution_id,
                                           uint64_t document, uint32_t node,
                                           const char *value,
                                           size_t value_length,
                                           char **error_out);
extern int goGossamerV8HostStyleProperty(
    uint64_t execution_id, uint64_t document, uint32_t node,
    const char *name, size_t name_length, char **value_out,
    size_t *value_length_out, char **priority_out, size_t *priority_length_out,
    int *found_out, char **error_out);
extern int goGossamerV8HostSetStyleProperty(
    uint64_t execution_id, uint64_t document, uint32_t node,
    const char *name, size_t name_length, const char *value,
    size_t value_length, const char *priority, size_t priority_length,
    char **error_out);
extern int goGossamerV8HostRemoveStyleProperty(
    uint64_t execution_id, uint64_t document, uint32_t node,
    const char *name, size_t name_length, char **value_out,
    size_t *value_length_out, char **error_out);
extern int goGossamerV8HostStylePropertyCount(uint64_t execution_id,
                                              uint64_t document,
                                              uint32_t node,
                                              size_t *count_out,
                                              char **error_out);
extern int goGossamerV8HostStylePropertyName(
    uint64_t execution_id, uint64_t document, uint32_t node, size_t index,
    char **name_out, size_t *name_length_out, int *found_out,
    char **error_out);
extern int goGossamerV8HostRetainNodeWrapper(uint64_t execution_id,
                                             uint64_t document, uint32_t node,
                                             char **error_out);
extern int goGossamerV8HostQueueCallback(uint64_t execution_id,
                                         uint64_t callback, char **error_out);
extern int goGossamerV8HostQueueMicrotask(uint64_t execution_id,
                                          uint64_t callback, char **error_out);
extern int goGossamerV8HostSetTimeout(uint64_t execution_id, uint64_t callback,
                                      int64_t delay_milliseconds,
                                      uint64_t *timer_out, char **error_out);
extern int goGossamerV8HostClearTimeout(uint64_t execution_id, uint64_t timer,
                                        char **error_out);

static gossamer_v8_host gossamer_v8_go_host(uint64_t execution_id) {
  gossamer_v8_host host = {
      .execution_id = execution_id,
      .document_metadata = goGossamerV8HostDocumentMetadata,
      .get_element_by_id = goGossamerV8HostGetElementByID,
      .create_element = goGossamerV8HostCreateElement,
      .create_element_ns = goGossamerV8HostCreateElementNS,
      .create_text_node = goGossamerV8HostCreateTextNode,
      .text_content = goGossamerV8HostTextContent,
      .set_text_content = goGossamerV8HostSetTextContent,
      .append_child = goGossamerV8HostAppendChild,
      .insert_before = goGossamerV8HostInsertBefore,
      .remove_child = goGossamerV8HostRemoveChild,
      .get_attribute = goGossamerV8HostGetAttribute,
      .set_attribute = goGossamerV8HostSetAttribute,
      .remove_attribute = goGossamerV8HostRemoveAttribute,
      .node_metadata = goGossamerV8HostNodeMetadata,
      .related_node = goGossamerV8HostRelatedNode,
      .child_nodes = goGossamerV8HostChildNodes,
      .contains = goGossamerV8HostContains,
      .replace_child = goGossamerV8HostReplaceChild,
      .node_value = goGossamerV8HostNodeValue,
      .set_node_value = goGossamerV8HostSetNodeValue,
      .has_attribute = goGossamerV8HostHasAttribute,
      .style_css_text = goGossamerV8HostStyleCSSText,
      .set_style_css_text = goGossamerV8HostSetStyleCSSText,
      .style_property = goGossamerV8HostStyleProperty,
      .set_style_property = goGossamerV8HostSetStyleProperty,
      .remove_style_property = goGossamerV8HostRemoveStyleProperty,
      .style_property_count = goGossamerV8HostStylePropertyCount,
      .style_property_name = goGossamerV8HostStylePropertyName,
      .retain_node_wrapper = goGossamerV8HostRetainNodeWrapper,
      .queue_callback = goGossamerV8HostQueueCallback,
      .queue_microtask = goGossamerV8HostQueueMicrotask,
      .set_timeout = goGossamerV8HostSetTimeout,
      .clear_timeout = goGossamerV8HostClearTimeout,
  };
  return host;
}

int gossamer_v8_go_realm_evaluate(gossamer_v8_realm *realm,
                                  uint64_t execution_id, const char *source,
                                  size_t source_length, const char *source_url,
                                  size_t source_url_length, char **error_out) {
  gossamer_v8_host host = gossamer_v8_go_host(execution_id);
  return gossamer_v8_realm_evaluate(realm, &host, source, source_length,
                                    source_url, source_url_length, error_out);
}

int gossamer_v8_go_realm_dispatch_event(gossamer_v8_realm *realm,
                                        uint64_t execution_id,
                                        uint8_t event_type, uint64_t document,
                                        uint32_t node, double x, double y,
                                        int32_t button, char **error_out) {
  gossamer_v8_host host = gossamer_v8_go_host(execution_id);
  return gossamer_v8_realm_dispatch_event(realm, &host, event_type, document,
                                          node, x, y, button, error_out);
}

int gossamer_v8_go_realm_invoke(gossamer_v8_realm *realm, uint64_t execution_id,
                                uint64_t callback, char **error_out) {
  gossamer_v8_host host = gossamer_v8_go_host(execution_id);
  return gossamer_v8_realm_invoke(realm, &host, callback, error_out);
}

int gossamer_v8_go_realm_drain_microtasks(gossamer_v8_realm *realm,
                                          uint64_t execution_id,
                                          char **error_out) {
  gossamer_v8_host host = gossamer_v8_go_host(execution_id);
  return gossamer_v8_realm_drain_microtasks(realm, &host, error_out);
}
