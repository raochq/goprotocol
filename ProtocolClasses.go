// 协议定义
// RegisterDataClass
package protocol

const (
	classID_SvrBase = 1000000
	SM_BASE         = 3000000
	CM_BASE         = 4000000
)
const (
	ClassID_Test     = 1000
	ClassID_SlotData = classID_SvrBase + 11
	ClassID_MapInfo  = classID_SvrBase + 12
)

// 地图本身
type TMapInfo struct {
	Idx          int32
	Name         string
	RefreshPoint uint16
	SlotList     []TSlotData
	//SlotList []*TSlotData
	//SlotList []interface{}
	MaxCount         int32
	MaxLineUpCount   int32
	MaxClerk         int32
	MaxCook          int32
	CookExp          int32
	OrderExp         int32
	DeliveryExp      int32
	PointRefreshTime int32
}

// 地图上的插槽
type TSlotData struct {
	Idx         int32
	SlotType    uint16
	BoSit       bool
	PlaceItemId int32
	SitPersonId int32
}

func RegisterProtocolClasses() {
	RegisterDataClass(ClassID_MapInfo, (*TMapInfo)(nil))
	RegisterDataClass(ClassID_SlotData, (*TSlotData)(nil))
}

func init() {
	RegisterProtocolClasses()
}
