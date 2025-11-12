//go:build ignore

#include "common.h"

char __license[] SEC("license") = "GPL";

// Ring buffer for sending events to userspace
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 20); // 1MB buffer
} events SEC(".maps");

// Syscall arguments structure for sys_enter_write tracepoint
struct sys_enter_write_args {
    // Common trace event fields
    unsigned short common_type;
    unsigned char common_flags;
    unsigned char common_preempt_count;
    int common_pid;

    // Syscall specific fields
    long __syscall_nr;
    unsigned long fd;
    unsigned long buf;
    unsigned long count;
};

// Tracepoint that captures write() syscalls
SEC("tracepoint/syscalls/sys_enter_write")
int trace_write(struct sys_enter_write_args *ctx) {
    // Get PID and TID
    u64 pid_tgid = bpf_get_current_pid_tgid();
    u32 pid = pid_tgid >> 32;
    u32 tid = pid_tgid & 0xFFFFFFFF;

    // Filter: Only monitor trim_sac process
    char comm[16];
    bpf_get_current_comm(&comm, sizeof(comm));

    // Check if this is trim_sac
    const char target_comm[] = "trim_sac";
    int match = 1;
    #pragma unroll
    for (int i = 0; i < sizeof(target_comm) - 1; i++) {
        if (comm[i] != target_comm[i]) {
            match = 0;
            break;
        }
    }

    // Skip if not trim_sac
    if (!match) {
        return 0;
    }

    // Skip stdin/stdout/stderr
    u32 fd = (u32)ctx->fd;
    if (fd < 3 || fd > 1024) {
        return 0;
    }

    // Reserve space in ring buffer
    struct sendmsg_event *event;
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }

    // Fill event data
    event->pid = pid;
    event->tid = tid;
    event->timestamp = bpf_ktime_get_ns();
    event->fd = fd;
    event->flags = 0;

    // Set socket path
    const char path[] = "trim_sac";
    #pragma unroll
    for (int i = 0; i < sizeof(path) && i < SOCKET_PATH_SIZE; i++) {
        event->socket_path[i] = path[i];
    }

    // Read data from write buffer
    event->data_len = 0;
    event->debug_step = 0;
    event->debug_iovlen = 0;

    // Calculate read length with bounds check
    u64 count64 = ctx->count;
    u32 read_len = 0;

    if (count64 > 0 && count64 < 0x7FFFFFFF) {
        read_len = (u32)count64;
        if (read_len > MAX_DATA_SIZE) {
            read_len = MAX_DATA_SIZE;
        }
    } else {
        event->data_len = 0;
        goto submit;
    }

    // Ensure read_len is bounded
    read_len = read_len & 0xFFF;  // Mask to 4096 bytes

    // Read from user space buffer
    const void *buf_ptr = (const void *)ctx->buf;
    if (bpf_probe_read_user(event->data, read_len, buf_ptr) == 0) {
        event->data_len = read_len;
        event->debug_step = 13;  // success
    } else {
        event->debug_step = 14;  // failed
    }

submit:

    // Check if this is an appStoreList response and modify reqID to invalidate it
    // Reduce search range to first 200 bytes to keep eBPF verifier happy
    if (event->data_len > 100) {
        const char pattern[] = "\"data\":{\"list\":[";

        // Search for pattern in the first 200 bytes only
        #pragma clang loop unroll(disable)
        for (int i = 0; i < 200 && i < event->data_len - 16; i++) {
            // Manual match for "data":{"list":[
            if (event->data[i] == '"' && event->data[i+1] == 'd' &&
                event->data[i+2] == 'a' && event->data[i+3] == 't' &&
                event->data[i+4] == 'a' && event->data[i+5] == '"' &&
                event->data[i+6] == ':' && event->data[i+7] == '{' &&
                event->data[i+8] == '"' && event->data[i+9] == 'l' &&
                event->data[i+10] == 'i' && event->data[i+11] == 's' &&
                event->data[i+12] == 't' && event->data[i+13] == '"' &&
                event->data[i+14] == ':' && event->data[i+15] == '[') {

                event->flags |= FLAG_APPSTORE;

                // Find and modify reqID - search in first 150 bytes only
                #pragma clang loop unroll(disable)
                for (int k = 0; k < 150 && k < event->data_len - 40; k++) {
                    // Match "reqid":"
                    if (event->data[k] == '"' && event->data[k+1] == 'r' &&
                        event->data[k+2] == 'e' && event->data[k+3] == 'q' &&
                        event->data[k+4] == 'i' && event->data[k+5] == 'd' &&
                        event->data[k+6] == '"' && event->data[k+7] == ':' &&
                        event->data[k+8] == '"') {

                        // Modify last 4 chars of reqID (at offset k+9+24)
                        int reqid_value_start = k + 9;
                        void *reqid_addr = (void *)(ctx->buf + reqid_value_start + 24);
                        const char invalid_suffix[] = "XXXX";
                        bpf_probe_write_user(reqid_addr, invalid_suffix, 4);
                        break;
                    }
                }
                break;
            }
        }
    }

    // Check if this is a notify message - search for "notify":[
    // Search in first 200 bytes to keep verifier happy
    if (event->data_len > 50) {
        // Search for pattern "notify":[
        #pragma clang loop unroll(disable)
        for (int i = 0; i < 200 && i < event->data_len - 10; i++) {
            // Manual match for "notify":[
            if (event->data[i] == '"' && event->data[i+1] == 'n' &&
                event->data[i+2] == 'o' && event->data[i+3] == 't' &&
                event->data[i+4] == 'i' && event->data[i+5] == 'f' &&
                event->data[i+6] == 'y' && event->data[i+7] == '"' &&
                event->data[i+8] == ':' && event->data[i+9] == '[') {

                event->flags |= FLAG_NOTIFY;
                break;
            }
        }
    }

    // Submit the event
    bpf_ringbuf_submit(event, 0);

    return 0;
}
