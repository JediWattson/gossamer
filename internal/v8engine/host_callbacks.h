#ifndef GOSSAMER_INTERNAL_V8ENGINE_HOST_CALLBACKS_H_
#define GOSSAMER_INTERNAL_V8ENGINE_HOST_CALLBACKS_H_

#include "bridge.h"

int gossamer_v8_go_realm_evaluate(gossamer_v8_realm *realm,
                                  uint64_t execution_id, const char *source,
                                  size_t source_length, const char *source_url,
                                  size_t source_url_length, char **error_out);
int gossamer_v8_go_realm_dispatch_event(gossamer_v8_realm *realm,
                                        uint64_t execution_id,
                                        const gossamer_v8_input_event *event,
                                        int *default_prevented_out,
                                        char **error_out);
int gossamer_v8_go_realm_invoke(gossamer_v8_realm *realm, uint64_t execution_id,
                                uint64_t callback, char **error_out);
int gossamer_v8_go_realm_drain_microtasks(gossamer_v8_realm *realm,
                                          uint64_t execution_id,
                                          char **error_out);

#endif // GOSSAMER_INTERNAL_V8ENGINE_HOST_CALLBACKS_H_
