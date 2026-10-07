// Package tui draws the console dashboard.
//
// It is the machine's face: what is on the physical console when nobody is
// logged in. The layout is a dense header, a grid of labelled values, and a log
// that scrolls at the bottom -- the shape someone standing at the machine can
// read at a glance: identity and state at the top, activity underneath.
//
// Two rules shape the drawing:
//
//   - The header and the grid are drawn ALWAYS, with n/a where the cluster has
//     not answered yet. It matters: during the long silent part of a boot, an
//     empty screen says nothing, while a grid of local facts says the machine
//     is alive and what it is doing.
//   - Every panel is padded to an exact number of display columns. On a real
//     console a line one column too long wraps and breaks the frame.
//
// The console it is written for is the one the machine has: 128x48 on these VMs,
// measured, and it adapts to narrower ones -- three columns of values, two, or
// one.
//
// Everything here is local to the machine. It opens no port and offers no
// service; it reads the node's own kubeconfig and draws.
package tui
