package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"unsafe"
)

// ProtocolDataHeaderWritter 序列化写入头
type ProtocolDataHeaderWritter struct {
	isValid      bool
	shortLenMode bool
	shortClassId bool
	headerLength uint16
	startPos     int
}

// 序列化写入
type ProtocolWritter struct {
	buf []byte
}

// NewProtocolWritter create new ProtocolWritter instance.
// bufSize is the initial cap for the internal buffer in bytes.
func NewProtocolWritter(bufSize int) *ProtocolWritter {
	if bufSize <= 0 {
		bufSize = 1024
	}
	return &ProtocolWritter{
		buf: make([]byte, 0, bufSize),
	}
}

// Bytes returns the number of bytes that have been written into the current buffer.
func (b *ProtocolWritter) Bytes() []byte { return b.buf[:] }

// Len returns how many bytes are unused in the buffer.
func (b *ProtocolWritter) Len() int { return len(b.buf) }

// Cap returns the cap of the buffer
func (b *ProtocolWritter) Cap() int { return cap(b.buf) }
func (b *ProtocolWritter) Reset() {
	b.buf = b.buf[:0]
}

func (b *ProtocolWritter) tryGrowByReslice(n int) (int, bool) {
	if l := len(b.buf); n <= cap(b.buf)-l {
		b.buf = b.buf[:l+n]
		return l, true
	}
	return 0, false
}

const maxInt = int(^uint(0) >> 1)

func (b *ProtocolWritter) grow(n int) int {
	m := b.Len()
	// Try to grow by means of a reslice.
	if i, ok := b.tryGrowByReslice(n); ok {
		return i
	}
	c := cap(b.buf)
	if c > maxInt-c-n {
		panic(bytes.ErrTooLarge)
	} else {
		// Not enough space anywhere, we need to allocate.
		buf := make([]byte, 2*c+n)
		copy(buf, b.buf[:])
		b.buf = buf
	}
	// Restore b.off and len(b.buf).
	b.buf = b.buf[:m+n]
	return m
}
func (b *ProtocolWritter) WriteByte(c byte) error {
	m, ok := b.tryGrowByReslice(1)
	if !ok {
		m = b.grow(1)
	}
	b.buf[m] = c
	return nil
}
func (b *ProtocolWritter) Write(p []byte) (n int, err error) {
	m, ok := b.tryGrowByReslice(len(p))
	if !ok {
		m = b.grow(len(p))
	}
	return copy(b.buf[m:], p), nil
}

// writeUint16 write a Uint16
func (b *ProtocolWritter) writeUint16(v uint16) {
	m, ok := b.tryGrowByReslice(2)
	if !ok {
		m = b.grow(2)
	}
	b.buf[m] = byte(v)
	b.buf[m+1] = byte(v >> 8)
}
func (b *ProtocolWritter) writeUint32(v uint32) {
	m, ok := b.tryGrowByReslice(4)
	if !ok {
		m = b.grow(4)
	}
	b.buf[m] = byte(v)
	b.buf[m+1] = byte(v >> 8)
	b.buf[m+2] = byte(v >> 16)
	b.buf[m+3] = byte(v >> 24)
}
func (b *ProtocolWritter) UpdateDataLength(datlen uint32, head *ProtocolDataHeaderWritter) uint32 {
	if !head.isValid {
		return 0
	}
	idx := head.startPos + 2 + 2 // Sign + ClassId
	if !head.shortClassId {
		idx += 2
	}
	if head.shortLenMode {
		if datlen > 0xFFFF {
			head.isValid = false
			return 0
		}
		binary.LittleEndian.PutUint16(b.buf[idx:], uint16(datlen))
		return 2
	} else {
		binary.LittleEndian.PutUint32(b.buf[idx:], datlen)
		return 4
	}
}
func (b *ProtocolWritter) WriteDataHead(rtti *TRegRttiData, head *ProtocolDataHeaderWritter) {
	head.isValid = false
	if rtti == nil {
		return
	}
	sign := cSignFlag
	cId := rtti.ClassId

	head.shortClassId = cId <= 0xFFFF
	head.shortLenMode = !rtti.BigData
	head.headerLength = 6
	head.startPos = b.Len()
	lenIdx := 4
	if !head.shortClassId {
		sign = sign | 1
		head.headerLength += 2
		lenIdx += 2
	}
	if !head.shortLenMode {
		sign = sign | 2
		head.headerLength += 2
	}
	b.writeUint16(sign)
	if head.shortClassId {
		b.writeUint16(uint16(cId))
	} else {
		b.writeUint32(cId)
	}
	if head.shortLenMode {
		b.writeUint16(0)
	} else {
		b.writeUint32(0)
	}
	head.isValid = true
}

// WriteString write the length of string into the buffer,
// then it write string data  into the buffer.
func (b *ProtocolWritter) writeString(s string) {
	l := uint16(len(s))
	b.writeUint16(l)
	if l > 0 {
		b.Write([]byte(s))
	}
}

func (b *ProtocolWritter) writeMemory(ptr unsafe.Pointer, len int) {
	if len <= 0 {
		return
	}
	sliceHeader := reflect.SliceHeader{Data: uintptr(ptr), Len: len, Cap: len}
	b.Write(*(*[]byte)(unsafe.Pointer(&sliceHeader)))
}

func (b *ProtocolWritter) writeAny(obj interface{}) (err error) {
	if rtti, ok := GetRegRttiDataFromObj(obj); ok {
		_, err = b.writeStruct(PtrOf(obj), rtti)
	} else {
		err = errors.New("object isn't register")
	}
	return
}
func (b *ProtocolWritter) WriteEmptyHeader() {
	b.writeUint16(cSignFlag)
	b.writeUint16(0)
	b.writeUint16(6)
}

//writeStruct Write a Strcut to bytes
func (b *ProtocolWritter) writeStruct(ptr unsafe.Pointer, rttiData *TRegRttiData) (n int, err error) {
	if rttiData == nil {
		return 0, errors.New("rttiData is nil")
	}
	headWritter := ProtocolDataHeaderWritter{}
	b.WriteDataHead(rttiData, &headWritter)
	if !headWritter.isValid {
		return 0, errors.New("write protocol head error")
	}
	if ptr == nil {
		return 0, nil
	}
	for idx := 0; idx < len(rttiData.FieldData); {
		rttiField := &rttiData.FieldData[idx]
		fieldPtr := unsafe.Pointer(uintptr(ptr) + rttiField.offset)
		if rttiField.podMergeCount <= 1 { // 未合并的字段
			if rttiField.isPod() {
				b.writeMemory(fieldPtr, rttiField.podSize)

			} else {
				switch rttiField.Kind {
				case reflect.String:
					b.writeString(*(*string)(fieldPtr))
				case reflect.Array:
					b.writeUint32(rttiField.arrayLen)
					fieldPtr = PtrOf((*emptyInterface)(fieldPtr))
					if isPod(rttiField.arrayKind) { // 元素是pod类型,可以直接写入
						b.writeMemory(fieldPtr, rttiField.podSize)
					} else {
						switch rttiField.arrayKind {
						case reflect.String:
							for i := 0; i < int(rttiField.arrayLen); i++ {
								b.writeString(*(*string)(unsafe.Pointer(uintptr(fieldPtr) + uintptr(i*rttiField.arraySize))))
							}
						case reflect.Struct:
							if filedRttiData, ok := GetRegRttiDataFromType(rttiField.arrayType); ok {
								for i := 0; i < int(rttiField.arrayLen); i++ {
									b.writeStruct(unsafe.Pointer(uintptr(fieldPtr)+uintptr(i*rttiField.arraySize)), filedRttiData)
								}
							}
						case reflect.Interface:
							for i := 0; i < int(rttiField.arrayLen); i++ {
								b.writeAny(*(*interface{})(unsafe.Pointer(uintptr(fieldPtr) + uintptr(i*rttiField.arraySize))))
							}
						case reflect.Ptr:
							if filedRttiData, ok := GetRegRttiDataFromType(rttiField.arrayType); ok {
								for i := 0; i < int(rttiField.arrayLen); i++ {
									arrPtr := unsafe.Pointer(uintptr(fieldPtr) + uintptr(i*rttiField.arraySize))
									arrPtr = *(*unsafe.Pointer)(arrPtr)
									if arrPtr == nil {
										b.WriteEmptyHeader()
									} else {
										b.writeStruct(arrPtr, filedRttiData)
									}
								}
							}
						}
					}
				case reflect.Slice:
					slicePtr := (*reflect.SliceHeader)(fieldPtr)
					b.writeUint32(uint32(slicePtr.Len))
					if slicePtr.Len == 0 {
						idx++
						continue
					}
					fieldPtr := unsafe.Pointer(slicePtr.Data)
					if isPod(rttiField.arrayKind) { // 元素是pod类型,可以直接写入
						fieldPtr = PtrOf((*emptyInterface)(fieldPtr))
						b.writeMemory(fieldPtr, slicePtr.Len*rttiField.arraySize)
					} else {
						switch rttiField.arrayKind {
						case reflect.String:
							for i := 0; i < slicePtr.Len; i++ {
								b.writeString(*(*string)(unsafe.Pointer(uintptr(fieldPtr) + uintptr(i*rttiField.arraySize))))
							}
						case reflect.Struct:
							if filedRttiData, ok := GetRegRttiDataFromType(rttiField.arrayType); ok {
								for i := 0; i < slicePtr.Len; i++ {
									b.writeStruct(unsafe.Pointer(uintptr(fieldPtr)+uintptr(i*rttiField.arraySize)), filedRttiData)
								}
							}
						case reflect.Interface:
							for i := 0; i < slicePtr.Len; i++ {
								b.writeAny(*(*interface{})(unsafe.Pointer(uintptr(fieldPtr) + uintptr(i*rttiField.arraySize))))
							}
						case reflect.Ptr:
							if filedRttiData, ok := GetRegRttiDataFromType(rttiField.arrayType); ok {
								for i := 0; i < slicePtr.Len; i++ {
									arrPtr := unsafe.Pointer(uintptr(fieldPtr) + uintptr(i*rttiField.arraySize))
									arrPtr = *(*unsafe.Pointer)(arrPtr)
									if arrPtr == nil {
										b.WriteEmptyHeader()
									} else {
										b.writeStruct(arrPtr, filedRttiData)
									}
								}
							}
						}

					}
				case reflect.Struct:
					if filedRttiData, ok := G_DataClass[rttiField.typeHash]; ok {
						b.writeStruct(fieldPtr, filedRttiData)
					}
				case reflect.Interface:
					b.writeAny(*(*interface{})(fieldPtr))
				case reflect.Ptr:
					if filedRttiData, ok := G_DataClass[rttiField.typeHash]; ok {
						fieldPtr = *(*unsafe.Pointer)(fieldPtr)
						if fieldPtr == nil {
							b.WriteEmptyHeader()
						} else {
							b.writeStruct(fieldPtr, filedRttiData)
						}
					}
				}
			}
			idx++
		} else { // 合并的 pod 类型，直接取 podSize
			b.writeMemory(fieldPtr, rttiField.podSize)
			idx += rttiField.podMergeCount
		}
	}
	b.UpdateDataLength(uint32(b.Len()-headWritter.startPos), &headWritter)

	if !headWritter.isValid { // 非bigData长度却超了
		if !rttiData.BigData {
			rttiData.BigData = true
			tmp := make([]byte, b.Len()-headWritter.startPos-int(headWritter.headerLength))
			copy(tmp, b.buf[headWritter.startPos+int(headWritter.headerLength):])

			b.buf = b.buf[:headWritter.startPos]
			b.WriteDataHead(rttiData, &headWritter)
			b.buf = append(b.buf[:headWritter.startPos], tmp...)
			b.UpdateDataLength(uint32(b.Len()-headWritter.startPos), &headWritter)
		} else {
			b.buf = b.buf[:headWritter.startPos]
			return 0, errors.New("非bigData长度却超了")
		}
	}
	return b.Len(), nil

}
