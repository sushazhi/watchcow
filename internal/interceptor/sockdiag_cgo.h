// SPDX-License-Identifier: GPL-2.0-or-later
// sockdiag_cgo.h - Header for CGO Unix socket peer inode lookup

#ifndef SOCKDIAG_CGO_H
#define SOCKDIAG_CGO_H

#include <stdint.h>

// Get peer inode for a given Unix socket inode using SOCK_DIAG
// Parameters:
//   target_inode - The inode number to look up
// Returns:
//   Peer inode number on success, 0 on failure
uint32_t get_peer_inode_cgo(uint32_t target_inode);

#endif // SOCKDIAG_CGO_H
