package firstboot

// NodeDashboardRBACPath is where the console dashboard's cluster-wide role is
// written before it is applied, for the same reason the CNI manifest is: if the
// apply fails, the file that says what should exist is on disk to be read.
const NodeDashboardRBACPath = "/etc/kubernetes/vates-node-dashboard.yaml"

// nodeDashboardRBAC grants the nodes the right to read events.
//
// The console dashboard runs on each node and shows that node's events, and a
// node may not read events: Kubernetes' Node authorizer lets a kubelet read its
// own Node object and the pods scheduled to it, and nothing beyond that. The
// command that fails says so plainly --
//
//	events is forbidden: User "system:node:vates-worker-1" cannot list
//	resource "events"
//
// but it fails at display time, on the console, where it is least useful to
// discover.
//
// Events are NOT namespaced by node, and RBAC cannot filter by field, so this
// cannot be narrowed to "the events of the pods on this node": the grant is read
// access to events cluster-wide, for every node. That is a real, if small,
// widening of what a node may read, and it is why it is a named role here
// rather than something folded silently into the kubelet's own credentials.
//
// Read only -- get, list, watch -- and nothing else.
const nodeDashboardRBAC = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: vates:node-dashboard
rules:
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: vates:node-dashboard
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: vates:node-dashboard
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: Group
    name: system:nodes
`

// NodeDashboardRBAC returns the manifest.
func NodeDashboardRBAC() []byte { return []byte(nodeDashboardRBAC) }
