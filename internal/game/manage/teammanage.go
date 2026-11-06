package manage

// TeamManage 对应 TeamManage.
//
// 上游即空类 (2024/1/18 占位, 至今无成员). 照搬空实现而不是省略,
// 是为了让房间装配链与上游一一对应, 后续上游若填充成员也有落点.
type TeamManage struct{}

// NewTeamManage 对应构造
func NewTeamManage() *TeamManage { return &TeamManage{} }
