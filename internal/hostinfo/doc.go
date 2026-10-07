// Package hostinfo reads what the machine knows about itself without asking
// Kubernetes.
//
// The console shows uptime, CPU, memory, the gateway, the DNS servers -- and
// none of those need an API server. Reading them from /proc and /etc also means
// the header is populated from the first second of a boot, before the cluster
// exists, which is when someone staring at the console most wants to know
// whether anything is happening.
package hostinfo
