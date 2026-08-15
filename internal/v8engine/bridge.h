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
} gossamer_v8_profile;

GOSSAMER_V8_EXPORT int gossamer_v8_initialize(const char *icu_data_path,
                                              char **error_out);
GOSSAMER_V8_EXPORT const char *gossamer_v8_version(void);

GOSSAMER_V8_EXPORT gossamer_v8_realm *
gossamer_v8_realm_new(uint64_t sampling_interval, char **error_out);
GOSSAMER_V8_EXPORT int
gossamer_v8_realm_evaluate(gossamer_v8_realm *realm, const char *source,
                           size_t source_length, const char *source_url,
                           size_t source_url_length, char **error_out);
GOSSAMER_V8_EXPORT int
gossamer_v8_realm_drain_microtasks(gossamer_v8_realm *realm, char **error_out);
GOSSAMER_V8_EXPORT int
gossamer_v8_realm_collect_garbage(gossamer_v8_realm *realm, char **error_out);
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
