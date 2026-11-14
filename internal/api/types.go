package api

// Status mirrors the shared `Status` protobuf message.
//
//	message Status {
//	  int64 request_id = 1;
//	  int32 code = 2; // 1 = success
//	  string msg = 3;
//	}
type Status struct {
	RequestID int64
	Code      int32
	Msg       string
}

// UserInfoData is the `data` payload of the /v1/user/info response.
type UserInfoData struct {
	ID                  string
	Name                string
	Field3              int32
	AvatarURL           string
	AvatarID            int64
	Phone               string
	Email               string
	Coin                float64
	IsVIP               bool
	VIPExpiredTimestamp int64
	VIPStatus           int32
	InvitationCode      string
}

// UserInfo is the decoded `UserInfo` protobuf message returned by
// GET /v1/user/info.
type UserInfo struct {
	Status Status
	Data   *UserInfoData
}
