//go:build v8 && cgo && darwin && arm64

#include "host_callbacks.h"

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
      .get_element_by_id = goGossamerV8HostGetElementByID,
      .create_element = goGossamerV8HostCreateElement,
      .create_text_node = goGossamerV8HostCreateTextNode,
      .text_content = goGossamerV8HostTextContent,
      .set_text_content = goGossamerV8HostSetTextContent,
      .append_child = goGossamerV8HostAppendChild,
      .insert_before = goGossamerV8HostInsertBefore,
      .remove_child = goGossamerV8HostRemoveChild,
      .get_attribute = goGossamerV8HostGetAttribute,
      .set_attribute = goGossamerV8HostSetAttribute,
      .remove_attribute = goGossamerV8HostRemoveAttribute,
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
