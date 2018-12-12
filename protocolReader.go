package protocol

import (
	"github.com/pkg/errors"
	"reflect"
	"unsafe"
)

type ProtocolDataHeader struct {
	isValid      bool
	headerLength uint16
	classId      uint32
	dataLength   uint32
}

type ProtocolReader struct {
	buf   []byte
	off   int
	Error error
}

// Parse creates an ProtocolReader instance from io.Reader
func NewProtocolReader(data []byte) *ProtocolReader {
	return &ProtocolReader{
		buf:   data,
		off:   0,
		Error: nil,
	}
}

// Len returns the number of bytes of the unread portion of the buffer;
func (reader *ProtocolReader) Len() int { return len(reader.buf) - reader.off }

// ReadDataHead load protocol head data
func (r *ProtocolReader) ReadDataHead(DataHeader *ProtocolDataHeader) {
	DataHeader.isValid = false
	DataHeader.headerLength = 0
	DataHeader.classId = 0xFFFFFFFF
	DataHeader.dataLength = 0
	if r.Len() < 6 {
		return
	}
	sign, ok := r.readUint16()
	if sign&cSignFlagMask != cSignFlag || !ok {
		return
	}
	var classIdSize uint16 = 2
	DataHeader.headerLength = 6
	if sign&1 == 1 {
		DataHeader.headerLength += 2
		classIdSize += 2
		if DataHeader.classId, ok = r.readUint32(); !ok {
			return
		}
	} else {
		if cid, ok := r.readUint16(); ok {
			DataHeader.classId = uint32(cid)
		} else {
			return
		}
	}
	if sign&2 == 2 {
		if r.Len() < 4 {
			return
		}
		DataHeader.headerLength += 2
		if DataHeader.dataLength, ok = r.readUint32(); !ok {
			return
		}
	} else {
		if r.Len() < 2 {
			return
		}
		if dataLen, ok := r.readUint16(); ok {
			DataHeader.dataLength = uint32(dataLen)
		} else {
			return
		}
	}
	if DataHeader.dataLength >= uint32(DataHeader.headerLength) {
		DataHeader.isValid = true
	}
}

// readAny decode binary into interface{}
func (r *ProtocolReader) readAny() (interface{}, error) {
	var dataHead ProtocolDataHeader
	starPos := r.off
	r.ReadDataHead(&dataHead)
	if !dataHead.isValid {
		return nil, errors.New("读取数据头错误")
	}
	if uint32(len(r.buf)-starPos) < dataHead.dataLength {
		return nil, errors.New("读取数据头错误")
	}
	var rttiData *TRegRttiData
	var ok bool
	if rttiData, ok = GetRegRttiDataByClassId(dataHead.classId); !ok {
		return nil, errors.New("object isn't register")
	}
	if dataHead.dataLength == uint32(dataHead.headerLength) {
		return nil, nil
	}
	val := reflect.New(rttiData.rType)
	if _, ok := r.readVal(dataHead, unsafe.Pointer(val.Pointer()), rttiData); ok {
		return val.Interface(), nil
	} else {
		return nil, errors.New("解析失败")
	}
}
func (r *ProtocolReader) readMemory(ptr unsafe.Pointer, len int) (n int) {
	if r.Len() < len {
		len = r.Len()
	}
	if len <= 0 {
		return 0
	}

	for i := 0; i < len; i += 64 {
		ss := (*[64]byte)(unsafe.Pointer(uintptr(ptr) + uintptr(i)*64))
		copy(ss[:], r.buf[r.off+i*64:r.off+len])
	}
	//sliceHeader := reflect.SliceHeader{Data: uintptr(ptr), Len: len, Cap: len}
	//copy(*(*[]byte)(unsafe.Pointer(&sliceHeader)), r.buf[r.off:r.off+len])
	r.off += len
	return len
}
func (r *ProtocolReader) readUint16() (ret uint16, ok bool) {
	ok = false
	_ = r.buf[r.off+1] // bounds check hint to compiler; see golang.org/issue/14808
	ret = uint16(r.buf[r.off]) | uint16(r.buf[r.off+1])<<8
	r.off += 2
	ok = true
	return
}
func (r *ProtocolReader) readUint32() (ret uint32, ok bool) {
	ok = false
	_ = r.buf[r.off+3] // bounds check hint to compiler; see golang.org/issue/14808
	ret = uint32(r.buf[r.off]) | uint32(r.buf[r.off+1])<<8 | uint32(r.buf[r.off+2])<<16 | uint32(r.buf[r.off+3])<<24
	r.off += 4
	ok = true
	return
}
func (r *ProtocolReader) readString() (s string, rn int, ok bool) {
	l := uint16(0)
	if l, ok = r.readUint16(); ok {
		rn = stringLenSize
		if l > 0 && r.Len() >= int(l) {
			s = string(r.buf[r.off : r.off+int(l)])
			rn += int(l)
			r.off += int(l)
		} else {
			ok = false
		}
	}
	return
}
func (r *ProtocolReader) readStruct(rttiType reflect.Type, ptr unsafe.Pointer) bool {
	var arrHead ProtocolDataHeader
	r.ReadDataHead(&arrHead)
	if !arrHead.isValid {
		return false
	}
	if filedRttiData, ok := GetRegRttiDataByClassId(arrHead.classId); !ok {
		return false
	} else if filedRttiData.rType != rttiType {
		return false
	} else {
		if _, ok := r.readVal(arrHead, ptr, filedRttiData); !ok {
			return false
		}
	}
	return true
}
func (r *ProtocolReader) readPointer(tyhash uintptr, ptr unsafe.Pointer) bool {

	var fieldHead ProtocolDataHeader
	r.ReadDataHead(&fieldHead)
	if !fieldHead.isValid {
		return false
	}
	if filedRttiData, ok := G_DataClass[tyhash]; !ok {
		return false
	} else if filedRttiData.ClassId != fieldHead.classId { // 因为是指针,这里通过rttiField.arrayType获取反射信息 而不是classid
		return false
	} else if fieldHead.dataLength > uint32(fieldHead.headerLength) { //只有非nil才处理
		if *(*unsafe.Pointer)(ptr) == nil {
			val := reflect.New(filedRttiData.rType)
			*(*uintptr)(ptr) = val.Pointer()
		}
		ptr = *(*unsafe.Pointer)(ptr)
		r.readVal(fieldHead, ptr, filedRttiData)
	}
	return true
}
func (r *ProtocolReader) readVal(dataHead ProtocolDataHeader, ptr unsafe.Pointer, rttiData *TRegRttiData) (n int, ok bool) {
	if rttiData == nil {
		return 0, false
	}
	datalen := int(dataHead.dataLength)
	readLen := int(dataHead.headerLength)
	startPos := r.off - readLen
	ok = false
	for idx := 0; idx < len(rttiData.FieldData); {
		if readLen >= datalen {
			break
		}
		rttiField := &rttiData.FieldData[idx]
		fieldPtr := unsafe.Pointer(uintptr(ptr) + rttiField.offset)
		if rttiField.podMergeCount <= 1 { // 未合并的字段
			if rttiField.isPod() {
				if rn := r.readMemory(fieldPtr, rttiField.podSize); rn != rttiField.podSize {
					readLen += rn
					break
				} else {
					readLen += rttiField.podSize
				}
			} else {
				switch rttiField.Kind {
				case reflect.String:
					if readLen+stringLenSize > datalen {
						break
					}
					slen := 0
					if *(*string)(fieldPtr), slen, ok = r.readString(); !ok {
						return r.off - startPos, false
					} else {
						readLen += slen
					}
				case reflect.Array:
					if readLen+arrayLenSize > datalen {
						break
					}
					arrlen, ok := r.readUint32()
					if !ok {
						return r.off - startPos, false
					}
					readLen += arrayLenSize
					fieldPtr = PtrOf((*emptyInterface)(fieldPtr))
					if isPod(rttiField.arrayKind) { // 元素是pod类型,可以直接写入
						arrDataLen := int(arrlen) * rttiField.arraySize
						if arrDataLen <= rttiField.podSize { // 固定长度的数组不可用溢出
							if r.readMemory(fieldPtr, arrDataLen) != arrDataLen {
								return r.off - startPos, false
							} else {
								readLen += arrDataLen
							}
						} else {
							newIdx := r.off + arrDataLen
							if r.readMemory(fieldPtr, rttiField.podSize) != rttiField.podSize {
								return r.off - startPos, false
							}
							r.off = newIdx
							readLen = r.off - startPos
						}
					} else {
						switch rttiField.arrayKind {
						case reflect.String:
							for i := 0; i < int(arrlen); i++ {
								if i < int(rttiField.arrayLen) {
									slen := 0
									if *(*string)(unsafe.Pointer(uintptr(fieldPtr) + uintptr(i*rttiField.arraySize))), slen, ok = r.readString(); !ok {
										return r.off - startPos, false
									} else {
										readLen += slen
									}
								} else { //超长直接跳过
									slen := 0
									if _, slen, ok = r.readString(); !ok {
										return r.off - startPos, false
									} else {
										readLen += slen
									}
								}
							}
						case reflect.Struct:
							for i := uint32(0); i < arrlen; i++ {
								if i < rttiField.arrayLen {
									if ok := r.readStruct(rttiField.arrayType, unsafe.Pointer(uintptr(fieldPtr)+uintptr(int(i)*rttiField.arraySize))); !ok {
										return r.off - startPos, false
									} else {
										readLen = r.off - startPos
									}
								} else {
									if _, err := r.readAny(); err != nil {
										return r.off - startPos, false
									} else {
										readLen = r.off - startPos
									}
								}
							}
						case reflect.Interface:
							for i := uint32(0); i < arrlen; i++ {
								if obj, err := r.readAny(); err != nil {
									return r.off - startPos, false
								} else {
									if i < rttiField.arrayLen {
										*(*interface{})(unsafe.Pointer(uintptr(fieldPtr) + uintptr(int(i)*rttiField.arraySize))) = obj
									}
									readLen = r.off - startPos
								}
							}
						case reflect.Ptr:
							for i := uint32(0); i < arrlen; i++ {
								if i < rttiField.arrayLen {
									if ok := r.readPointer(uintptr(PtrOf(rttiField.arrayType)), unsafe.Pointer(uintptr(fieldPtr)+uintptr(int(i)*rttiField.arraySize))); !ok {
										return r.off - startPos, false
									} else {
										readLen = r.off - startPos
									}
								} else {
									if _, err := r.readAny(); err != nil {
										return r.off - startPos, false
									} else {
										readLen = r.off - startPos
									}
								}
							}
						}

					}
				case reflect.Slice:
					if readLen+arrayLenSize > datalen {
						break
					}
					arrlen := uint32(0)
					if arrlen, ok = r.readUint32(); !ok {
						return r.off - startPos, false
					}
					readLen += arrayLenSize
					if arrlen == 0 {
						idx++
						continue
					}
					slicePtr := (*reflect.SliceHeader)(fieldPtr)
					fieldData := reflect.MakeSlice(rttiField.rType, int(arrlen), int(arrlen)).Interface()
					tp := (*reflect.SliceHeader)(PtrOf(fieldData))
					*slicePtr = *(*reflect.SliceHeader)(tp)
					fieldPtr = unsafe.Pointer(slicePtr.Data)
					if isPod(rttiField.arrayKind) { // 元素是pod类型,可以直接写入
						arrDataLen := slicePtr.Len * rttiField.arraySize
						if r.readMemory(fieldPtr, arrDataLen) != arrDataLen {
							return r.off - startPos, false
						} else {
							readLen += arrDataLen
						}
					} else {
						switch rttiField.arrayKind {
						case reflect.String:
							for i := 0; i < int(arrlen); i++ {
								slen := 0
								if *(*string)(unsafe.Pointer(uintptr(fieldPtr) + uintptr(int(i)*rttiField.arraySize))), slen, ok = r.readString(); !ok {
									return r.off - startPos, false
								} else {
									readLen += slen
								}
							}
						case reflect.Struct:
							for i := 0; i < int(arrlen); i++ {
								if ok := r.readStruct(rttiField.arrayType, unsafe.Pointer(uintptr(fieldPtr)+uintptr(int(i)*rttiField.arraySize))); !ok {
									return r.off - startPos, false
								} else {
									readLen = r.off - startPos
								}
							}
						case reflect.Interface:
							for i := 0; i < int(arrlen); i++ {
								if obj, err := r.readAny(); err != nil {
									return r.off - startPos, false
								} else {
									*(*interface{})(unsafe.Pointer(uintptr(fieldPtr) + uintptr(int(i)*rttiField.arraySize))) = obj
									readLen = r.off - startPos
								}
							}
						case reflect.Ptr:
							for i := 0; i < int(arrlen); i++ {
								if ok := r.readPointer(uintptr(PtrOf(rttiField.arrayType)), unsafe.Pointer(uintptr(fieldPtr)+uintptr(int(i)*rttiField.arraySize))); !ok {
									return r.off - startPos, false
								} else {
									readLen = r.off - startPos
								}
							}
						}

					}
				case reflect.Struct:
					if ok := r.readStruct(rttiField.rType, fieldPtr); !ok {
						return r.off - startPos, false
					} else {
						readLen = r.off - startPos
					}
				case reflect.Interface:
					if obj, err := r.readAny(); err != nil {
						return r.off - startPos, false
					} else {
						*(*interface{})(fieldPtr) = obj
						readLen = r.off - startPos
					}
				case reflect.Ptr:
					if ok := r.readPointer(rttiField.typeHash, fieldPtr); !ok {
						return r.off - startPos, false
					} else {
						readLen = r.off - startPos
					}
				}
			}
			idx++
		} else { // 合并的 pod 类型，直接取 podSize
			if rn := r.readMemory(fieldPtr, rttiField.podSize); rn != rttiField.podSize {
				readLen += rn
				break
			} else {
				readLen += rn
			}
			idx += rttiField.podMergeCount
		}
	}

	r.off = startPos + int(dataHead.dataLength)
	return r.off - startPos, true
}
