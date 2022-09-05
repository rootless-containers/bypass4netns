package api

type BypassStatus struct {
	ID   string     `json:"id"`
	Pid  int        `json:"pid"`
	Spec BypassSpec `json:"spec"`
}

type BypassSpec struct {
	ID            string     `json:"id"`
	SocketPath    string     `json:"socketPath"`
	PidFilePath   string     `json:"pidFilePath"`
	LogFilePath   string     `json:"logFilePath"`
	PortMapping   []PortSpec `json:"portMapping"`
	IgnoreSubnets []string   `json:"ignoreSubnets"` // CIDR or "auto"
}

type PortSpec struct {
	Protos     []string `json:"protos"`
	ParentIP   string   `json:"parentIP"`
	ParentPort int      `json:"parentPort"`
	ChildIP    string   `json:"childIP"`
	ChildPort  int      `json:"childPort"`
}

type ErrorJSON struct {
	Message string `json:"message"`
}
