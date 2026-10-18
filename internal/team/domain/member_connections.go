package domain

// MemberConnections is a directory summary of member-supplied availability. Unknown
// includes older devices that have not reported connection data and stale reports.
type MemberConnections struct {
	RunnersConnected    int  `json:"runnersConnected"`
	RunnersDisconnected int  `json:"runnersDisconnected"`
	RunnersUnknown      int  `json:"runnersUnknown"`
	PhonesConnected     int  `json:"phonesConnected"`
	PhonesPaired        int  `json:"phonesPaired"`
	PhonesUnknown       bool `json:"phonesUnknown"`
}
