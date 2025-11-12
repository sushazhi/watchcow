/* Common eBPF headers and definitions */

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <linux/types.h>
#include <linux/socket.h>
#include <linux/un.h>

// Type aliases for compatibility
typedef __u64 u64;
typedef __u32 u32;
typedef __u16 u16;
typedef __u8 u8;

// Section macro
#ifndef SEC
#define SEC(NAME) __attribute__((section(NAME), used))
#endif

// Constants
#define MAX_DATA_SIZE 4096
#define SOCKET_PATH_SIZE 108

// Event flags
#define FLAG_APPSTORE 0x01  // Contains appStoreList response
#define FLAG_NOTIFY   0x02  // Contains notify message

// Event structure sent to userspace
struct sendmsg_event {
    __u32 pid;
    __u32 tid;
    __u32 fd;
    __u32 data_len;
    __u64 timestamp;
    __u32 debug_step;
    __u32 debug_iovlen;
    __u32 flags;  // Event flags (FLAG_APPSTORE, etc)
    char socket_path[SOCKET_PATH_SIZE];
    char data[MAX_DATA_SIZE];
} __attribute__((packed));
