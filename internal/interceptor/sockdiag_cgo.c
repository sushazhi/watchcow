// SPDX-License-Identifier: GPL-2.0-or-later
// sockdiag_cgo.c - CGO wrapper for Unix socket peer inode lookup using SOCK_DIAG

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/socket.h>
#include <linux/netlink.h>
#include <linux/rtnetlink.h>
#include <linux/sock_diag.h>
#include <linux/unix_diag.h>
#include <errno.h>

// Helper macro to align netlink message length
#define NLMSG_ALIGN_LEN(len) (((len) + NLMSG_ALIGNTO - 1) & ~(NLMSG_ALIGNTO - 1))

// RTA_* helper macros (from rtnetlink.h)
#define RTA_NEXT_PTR(rta,attrlen) \
    ((attrlen) -= RTA_ALIGN((rta)->rta_len), \
     (struct rtattr*)(((char*)(rta)) + RTA_ALIGN((rta)->rta_len)))

// Parse rtattr into array
static void parse_rtattr(struct rtattr **tb, int max, struct rtattr *rta, int len)
{
    memset(tb, 0, sizeof(struct rtattr *) * (max + 1));

    while (RTA_OK(rta, len)) {
        if (rta->rta_type <= max) {
            tb[rta->rta_type] = rta;
        }
        rta = RTA_NEXT(rta, len);
    }
}

// Main function to get peer inode using SOCK_DIAG
// Returns peer inode on success, 0 on failure
uint32_t get_peer_inode_cgo(uint32_t target_inode)
{
    int fd = -1;
    uint32_t peer_inode = 0;

    // Create netlink socket
    fd = socket(AF_NETLINK, SOCK_RAW, NETLINK_SOCK_DIAG);
    if (fd < 0) {
        fprintf(stderr, "[CGO] Failed to create netlink socket: %s\n", strerror(errno));
        return 0;
    }

    // Bind socket
    struct sockaddr_nl nladdr = {
        .nl_family = AF_NETLINK,
        .nl_pad = 0,
        .nl_pid = 0,
        .nl_groups = 0
    };

    if (bind(fd, (struct sockaddr *)&nladdr, sizeof(nladdr)) < 0) {
        fprintf(stderr, "[CGO] Failed to bind netlink socket: %s\n", strerror(errno));
        close(fd);
        return 0;
    }

    // Prepare SOCK_DIAG request
    struct {
        struct nlmsghdr nlh;
        struct unix_diag_req r;
    } req = {
        .nlh = {
            .nlmsg_len = sizeof(req),
            .nlmsg_type = SOCK_DIAG_BY_FAMILY,
            .nlmsg_flags = NLM_F_REQUEST | NLM_F_DUMP,
            .nlmsg_seq = 1,
            .nlmsg_pid = 0
        },
        .r = {
            .sdiag_family = AF_UNIX,
            .sdiag_protocol = 0,
            .pad = 0,
            .udiag_states = 0xFFFFFFFF,  // All states
            .udiag_ino = 0,
            .udiag_show = UDIAG_SHOW_PEER,  // Request peer inode
            .udiag_cookie = {0, 0}
        }
    };

    // Send request
    if (send(fd, &req, sizeof(req), 0) < 0) {
        fprintf(stderr, "[CGO] Failed to send netlink request: %s\n", strerror(errno));
        close(fd);
        return 0;
    }

    // Receive and parse responses
    char buf[8192];
    int done = 0;

    while (!done) {
        ssize_t len = recv(fd, buf, sizeof(buf), 0);
        if (len < 0) {
            if (errno == EINTR || errno == EAGAIN) {
                continue;
            }
            fprintf(stderr, "[CGO] Failed to receive netlink response: %s\n", strerror(errno));
            break;
        }

        if (len == 0) {
            break;
        }

        // Parse netlink messages
        struct nlmsghdr *nlh = (struct nlmsghdr *)buf;

        while (NLMSG_OK(nlh, len)) {
            // Check for end of dump
            if (nlh->nlmsg_type == NLMSG_DONE) {
                done = 1;
                break;
            }

            // Check for error
            if (nlh->nlmsg_type == NLMSG_ERROR) {
                struct nlmsgerr *err = (struct nlmsgerr *)NLMSG_DATA(nlh);
                fprintf(stderr, "[CGO] Netlink error: %s\n", strerror(-err->error));
                done = 1;
                break;
            }

            // Parse unix_diag_msg
            if (nlh->nlmsg_type == SOCK_DIAG_BY_FAMILY) {
                struct unix_diag_msg *diag = (struct unix_diag_msg *)NLMSG_DATA(nlh);
                int rta_len = nlh->nlmsg_len - NLMSG_LENGTH(sizeof(*diag));

                // Check if this is our target inode
                if (diag->udiag_ino == target_inode) {
                    // Parse rtattrs
                    struct rtattr *tb[UNIX_DIAG_MAX + 1];
                    parse_rtattr(tb, UNIX_DIAG_MAX,
                                (struct rtattr *)(diag + 1), rta_len);

                    // Extract peer inode
                    if (tb[UNIX_DIAG_PEER]) {
                        peer_inode = *(uint32_t *)RTA_DATA(tb[UNIX_DIAG_PEER]);
                        done = 1;
                        break;
                    }
                }
            }

            nlh = NLMSG_NEXT(nlh, len);
        }
    }

    close(fd);
    return peer_inode;
}
