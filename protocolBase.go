// 协议序列化和反序列的实现
// 作者 饶长泉
// 考虑到兼容各个平台,数值类型不使用变长类型 int 和uint
// string必须小于65536
// 考虑到其他平台的支持情况,目前只支持一维数组和切片
// 会导所有成员的数据
// 允许将结构体中的数组改为切片,若将切片成员改为数组,或将原数组长度改小.可以正常解析,但会丢失数据
// 一旦数据结构确定,只能在最后追加成员,不允许删除成员,允许修改成员名字,但不允许私有成员和公有成员互相转换
// 不支持成员为unsafe.Pointer,*interface{}对象
// 不支持interface成员中存放结构体外的类型
// 切片或者数值中存放的是指针时,只允许存放指向已注册的结构体
package protocol

import (
	"reflect"
	"unsafe"
)

const (
	/*
			数据头标记，使用两个字节存储，前14bit用于校验，最后2bit用于标记
		    0xFFFC = 1111 1111   1111 1100
		    0x6D98 = 0110 1101   1001 1000
		    最后两位用来标识DataLength长度(bit 1)和ClassId长度(bit 0)
	*/
	cSignFlagMask uint16 = 0xFFFC
	cSignFlag     uint16 = 0x6D98 // 序列化 Sign
	stringLenSize        = 2
	arrayLenSize         = 4
)

type TRegRttiData struct {
	ClassId   uint32
	BigData   bool
	rType     reflect.Type
	FieldData []TRegFieldOffsetData
}
type TRegFieldOffsetData struct {
	rType         reflect.Type
	Kind          reflect.Kind
	typeHash      uintptr
	offset        uintptr      // 相对于 Self 的偏移
	podMergeCount int          // 如果是合并字段，那么此值大于1，否则为0或1
	podSize       int          // 最终处理完毕后，当上一个字段有效时，此字段为合并值，否则为自身的大小
	arraySize     int          // 数组和切片类型的元素类型大小
	arrayLen      uint32       // 数组类型元素长度
	arrayKind     reflect.Kind // 数组和切片类型的元素类型
	arrayType     reflect.Type // 数组和切片类型的元素类型

}

var G_ClassId = make(map[uint32]*TRegRttiData)
var G_DataClass = make(map[uintptr]*TRegRttiData)

type IMsg interface{}

func (this *TRegFieldOffsetData) isPod() bool {
	return isPod(this.Kind)
}

func isPod(kd reflect.Kind) bool {
	return kd >= reflect.Bool && kd <= reflect.Float64
}
func GetClassId(msg IMsg) uint32 {
	if rtti, ok := GetRegRttiDataFromObj(msg); ok {
		return rtti.ClassId
	}
	return 0
}

func GetRegRttiDataFromObj(msg IMsg) (*TRegRttiData, bool) {
	tp := reflect.TypeOf(msg)
	if tp.Kind() == reflect.Ptr {
		tp = tp.Elem()
	}
	if rtti, ok := G_DataClass[uintptr(PtrOf(tp))]; ok {
		return rtti, true
	}
	return nil, false
}
func GetRegRttiDataFromType(tp reflect.Type) (*TRegRttiData, bool) {
	if rtti, ok := G_DataClass[uintptr(PtrOf(tp))]; ok {
		return rtti, true
	}
	return nil, false
}
func GetRegRttiDataByClassId(classid uint32) (dat *TRegRttiData, ok bool) {
	dat, ok = G_ClassId[classid]
	return
}
func GetDataClass(classid uint32) reflect.Type {
	if rtti, ok := G_ClassId[classid]; ok {
		return rtti.rType
	}
	return nil
}

// RegisterDataClass 注册函数
func RegisterDataClass(msgid uint32, msg IMsg) {
	// 只允许注册一次
	if _, ok := G_ClassId[msgid]; ok {
		return
	}
	tp := reflect.TypeOf(msg)
	rType := uintptr(PtrOf(tp)) // 类型hash
	pType := rType              // 对应指针hash
	if tp.Kind() == reflect.Ptr {
		tp = tp.Elem()
		rType = uintptr(PtrOf(tp))
	} else {
		pType = uintptr(PtrOf(reflect.New(tp).Type()))
	}
	rtti := &TRegRttiData{ClassId: msgid, rType: tp, BigData: false}
	rtti.FieldData = make([]TRegFieldOffsetData, tp.NumField())
	sumOffset := uintptr(0)
	mergeIdx := 0
	for i := 0; i < tp.NumField(); i++ {
		fd := tp.Field(i)
		ftp := fd.Type

		//fmt.Println(uintptr(PtrOf(ftp)))
		rtti.FieldData[i] = TRegFieldOffsetData{rType: ftp, typeHash: uintptr(((*emptyInterface)(unsafe.Pointer(&ftp))).data), offset: fd.Offset, podMergeCount: 1, podSize: int(ftp.Size()), arraySize: 0, Kind: ftp.Kind()}
		if !rtti.FieldData[i].isPod() {
			switch rtti.FieldData[i].Kind {
			case reflect.Slice:
				val := ftp.Elem()
				rtti.FieldData[i].arrayKind = val.Kind()
				rtti.FieldData[i].arraySize = int(val.Size())
				rtti.FieldData[i].arrayType = val
			case reflect.Array:
				rtti.FieldData[i].arrayLen = uint32(ftp.Len())
				val := ftp.Elem()
				rtti.FieldData[i].arrayKind = val.Kind()
				rtti.FieldData[i].arraySize = int(val.Size())
				rtti.FieldData[i].arrayType = val
			}
			continue
		}
		if sumOffset == 0 {
			sumOffset = rtti.FieldData[i].offset + uintptr(rtti.FieldData[i].podSize)
			mergeIdx = i
		} else {
			if sumOffset == rtti.FieldData[i].offset {
				rtti.FieldData[mergeIdx].podMergeCount++
				rtti.FieldData[mergeIdx].podSize += int(ftp.Size())
				sumOffset += uintptr(rtti.FieldData[i].podSize)
			} else { // 当前内存不再连续，将当前字段设置为新的合并字段
				sumOffset = rtti.FieldData[i].offset + uintptr(rtti.FieldData[i].podSize)
				mergeIdx = i
			}
		}
	}
	G_ClassId[msgid] = rtti
	G_DataClass[rType] = rtti
	G_DataClass[pType] = rtti
}

type emptyInterface struct {
	rtype unsafe.Pointer
	data  unsafe.Pointer
}

func PtrOf(obj interface{}) unsafe.Pointer {
	return (*emptyInterface)(unsafe.Pointer(&obj)).data
}
func RTypeOf(obj interface{}) uintptr {
	return uintptr((*emptyInterface)(unsafe.Pointer(&obj)).rtype)
}

func Marshal(v interface{}) ([]byte, error) {
	writter := NewProtocolWritter(10)
	if err := writter.writeAny(v); err == nil {
		return writter.Bytes(), nil
	} else {
		return nil, err
	}
}

func Unmarshal(data []byte) (interface{}, error) {
	Reader := NewProtocolReader(data)
	return Reader.readAny()
}
