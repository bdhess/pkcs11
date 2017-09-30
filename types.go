// Copyright 2013 Miek Gieben. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkcs11

/*
#define CK_PTR *
#ifndef NULL_PTR
#define NULL_PTR 0
#endif
#define CK_DEFINE_FUNCTION(returnType, name) returnType name
#define CK_DECLARE_FUNCTION(returnType, name) returnType name
#define CK_DECLARE_FUNCTION_POINTER(returnType, name) returnType (* name)
#define CK_CALLBACK_FUNCTION(returnType, name) returnType (* name)

#include <stdlib.h>
#include <string.h>
#include "pkcs11.h"

CK_ULONG Index(CK_ULONG_PTR array, CK_ULONG i)
{
	return array[i];
}
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

type arena []unsafe.Pointer

func (a *arena) Allocate(obj []byte) (C.CK_VOID_PTR, C.CK_ULONG) {
	cobj := C.calloc(C.size_t(len(obj)), 1)
	*a = append(*a, cobj)
	C.memmove(cobj, unsafe.Pointer(&obj[0]), C.size_t(len(obj)))
	return C.CK_VOID_PTR(cobj), C.CK_ULONG(len(obj))
}

func (a arena) Free() {
	for _, p := range a {
		C.free(p)
	}
}

// toList converts from a C style array to a []uint.
func toList(clist C.CK_ULONG_PTR, size C.CK_ULONG) []uint {
	l := make([]uint, int(size))
	for i := 0; i < len(l); i++ {
		l[i] = uint(C.Index(clist, C.CK_ULONG(i)))
	}
	defer C.free(unsafe.Pointer(clist))
	return l
}

// cBBool converts a bool to a CK_BBOOL.
func cBBool(x bool) C.CK_BBOOL {
	if x {
		return C.CK_BBOOL(C.CK_TRUE)
	}
	return C.CK_BBOOL(C.CK_FALSE)
}

func uintToBytes(x uint64) []byte {
	ul := C.CK_ULONG(x)
	return C.GoBytes(unsafe.Pointer(&ul), C.int(unsafe.Sizeof(ul)))
}

func uintArrayToBytes(x []uint) []byte {
	ulSize := int(unsafe.Sizeof(C.CK_ULONG(0)))
	b := make([]byte, len(x)*ulSize)
	for i := 0; i < len(x); i++ {
		v := uintToBytes(uint64(x[i]))
		for j := 0; j < ulSize; j++ {
			b[(i*ulSize)+j] = v[j]
		}
	}
	return b
}

func attributesToBytes(x []*Attribute) []byte {
	arena, ptr, size := cAttributeList(x)
	bytes := C.GoBytes(unsafe.Pointer(&ptr), C.int(size))
	arena.Free()
	return bytes
}

// Error represents an PKCS#11 error.
type Error uint

func (e Error) Error() string {
	return fmt.Sprintf("pkcs11: 0x%X: %s", uint(e), strerror[uint(e)])
}

func toError(e C.CK_RV) error {
	if e == C.CKR_OK {
		return nil
	}
	return Error(e)
}

/* SessionHandle is a Cryptoki-assigned value that identifies a session. */
type SessionHandle uint

/* ObjectHandle is a token-specific identifier for an object.  */
type ObjectHandle uint

// Version represents any version information from the library.
type Version struct {
	Major byte
	Minor byte
}

func toVersion(version C.CK_VERSION) Version {
	return Version{byte(version.major), byte(version.minor)}
}

// SlotEvent holds the SlotID which for which an slot event (token insertion,
// removal, etc.) occurred.
type SlotEvent struct {
	SlotID uint
}

// Info provides information about the library and hardware used.
type Info struct {
	CryptokiVersion    Version
	ManufacturerID     string
	Flags              uint
	LibraryDescription string
	LibraryVersion     Version
}

/* SlotInfo provides information about a slot. */
type SlotInfo struct {
	SlotDescription string // 64 bytes.
	ManufacturerID  string // 32 bytes.
	Flags           uint
	HardwareVersion Version
	FirmwareVersion Version
}

/* TokenInfo provides information about a token. */
type TokenInfo struct {
	Label              string
	ManufacturerID     string
	Model              string
	SerialNumber       string
	Flags              uint
	MaxSessionCount    uint
	SessionCount       uint
	MaxRwSessionCount  uint
	RwSessionCount     uint
	MaxPinLen          uint
	MinPinLen          uint
	TotalPublicMemory  uint
	FreePublicMemory   uint
	TotalPrivateMemory uint
	FreePrivateMemory  uint
	HardwareVersion    Version
	FirmwareVersion    Version
	UTCTime            string
}

/* SesionInfo provides information about a session. */
type SessionInfo struct {
	SlotID      uint
	State       uint
	Flags       uint
	DeviceError uint
}

// Attribute holds an attribute type/value combination.
type Attribute struct {
	Type  uint
	Value []byte
}

// NewAttribute allocates a Attribute and returns a pointer to it.
// Note that this is merely a convience function, as values returned
// from the HSM are not converted back to Go values, those are just raw
// byte slices.
func NewAttribute(typ uint, x interface{}) *Attribute {
	// This function nicely transforms *to* an attribute, but there is
	// no corresponding function that transform back *from* an attribute,
	// which in PKCS#11 is just an byte array.
	a := new(Attribute)
	a.Type = typ
	if x == nil {
		return a
	}
	switch v := x.(type) {
	case bool:
		if v {
			a.Value = []byte{1}
		} else {
			a.Value = []byte{0}
		}
	case int:
		a.Value = uintToBytes(uint64(v))
	case uint:
		a.Value = uintToBytes(uint64(v))
	case []uint:
		a.Value = uintArrayToBytes([]uint(v))
	case []*Attribute:
		a.Value = attributesToBytes([]*Attribute(v))
	case string:
		a.Value = []byte(v)
	case []byte:
		a.Value = v
	case time.Time: // for CKA_DATE
		a.Value = cDate(v)
	default:
		panic("pkcs11: unhandled attribute type")
	}
	return a
}

// cAttribute returns the start address and the length of an attribute list.
func cAttributeList(a []*Attribute) (arena, C.CK_ATTRIBUTE_PTR, C.CK_ULONG) {
	var arena arena
	if len(a) == 0 {
		return nil, nil, 0
	}
	pa := make([]C.CK_ATTRIBUTE, len(a))
	for i := 0; i < len(a); i++ {
		pa[i]._type = C.CK_ATTRIBUTE_TYPE(a[i].Type)
		//skip attribute if length is 0 to prevent panic in arena.Allocate
		if a[i].Value == nil || len(a[i].Value) == 0 {
			continue
		}

		pa[i].pValue, pa[i].ulValueLen = arena.Allocate(a[i].Value)
	}
	return arena, C.CK_ATTRIBUTE_PTR(&pa[0]), C.CK_ULONG(len(a))
}

func cDate(t time.Time) []byte {
	b := make([]byte, 8)
	year, month, day := t.Date()
	y := fmt.Sprintf("%4d", year)
	m := fmt.Sprintf("%02d", month)
	d1 := fmt.Sprintf("%02d", day)
	b[0], b[1], b[2], b[3] = y[0], y[1], y[2], y[3]
	b[4], b[5] = m[0], m[1]
	b[6], b[7] = d1[0], d1[1]
	return b
}

// Mechanism holds an mechanism type/value combination.
type Mechanism struct {
	Mechanism uint
	Parameter interface{}
}

func NewMechanism(mech uint, x interface{}) *Mechanism {
	m := new(Mechanism)
	m.Mechanism = mech
	m.Parameter = x
	return m
}

func cMechanismList(m []*Mechanism) (arena, C.CK_MECHANISM_PTR, C.CK_ULONG) {
	var arena arena
	if len(m) == 0 {
		return nil, nil, 0
	}
	pm := make([]C.CK_MECHANISM, len(m))
	for i := 0; i < len(m); i++ {
		pm[i].mechanism = C.CK_MECHANISM_TYPE(m[i].Mechanism)
		param := m[i].Parameter
		if param == nil {
			continue
		}
		if nativeParam, ok := m[i].Parameter.(mechanismParam); ok {
			pm[i].pParameter, pm[i].ulParameterLen = nativeParam.toNative(arena)
		} else {
			bytesParam := m[i].Parameter.([]byte)
			//skip parameter if length is 0 to prevent panic in arena.Allocate
			if len(bytesParam) == 0 {
				continue
			}

			pm[i].pParameter, pm[i].ulParameterLen = arena.Allocate(bytesParam)
		}
	}
	return arena, C.CK_MECHANISM_PTR(&pm[0]), C.CK_ULONG(len(m))
}

// MechanismInfo provides information about a particular mechanism.
type MechanismInfo struct {
	MinKeySize uint
	MaxKeySize uint
	Flags      uint
}

type mechanismParam interface {
	toNative(arena) (C.CK_VOID_PTR, C.CK_ULONG)
}

// OaepParams provides wrapped access to CK_RSA_PKCS_OAEP_PARAMS
type OaepParams struct {
	HashAlgorithm          uint
	MaskGenerationFunction uint
	SourceType             uint
	Source                 []byte
}

func (p *OaepParams) toNative(arena arena) (C.CK_VOID_PTR, C.CK_ULONG) {
	var params *C.CK_RSA_PKCS_OAEP_PARAMS
	params.hashAlg = C.CK_MECHANISM_TYPE(p.HashAlgorithm)
	params.mgf = C.CK_RSA_PKCS_MGF_TYPE(p.MaskGenerationFunction)
	params.source = C.CK_RSA_PKCS_OAEP_SOURCE_TYPE(p.SourceType)
	if p.Source != nil {
		params.pSourceData, params.ulSourceDataLen = arena.Allocate(p.Source)
	}
	return C.CK_VOID_PTR(params), C.CK_ULONG(unsafe.Sizeof(params))
}
