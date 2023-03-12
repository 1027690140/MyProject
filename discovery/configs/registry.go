package configs

type Zone struct {
	Src string         `json:"src"`
	Dst map[string]int `json:"dst"`
}

type Scheduler struct {
	AppID  string `json:"app_id,omitempty"`
	Env    string `json:"env"`
	Zones  []Zone `json:"zones"` // zone-ratio
	Remark string `json:"remark"`
}

// Env is disocvery env.
