//go:build darwin

package secref

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	errSecSuccess      int32  = 0
	errSecItemNotFound int32  = -25300
	secLabelItemAttr   uint32 = 0x6c61626c // 'labl'
)

type secKeychainAttribute struct {
	tag    uint32
	length uint32
	data   uintptr
}

type secKeychainAttributeList struct {
	count      uint32
	attributes uintptr
}

type darwinKeychainStore struct {
	once sync.Once
	err  error

	addGenericPassword    func(uintptr, uint32, *byte, uint32, *byte, uint32, *byte, *uintptr) int32
	findGenericPassword   func(uintptr, uint32, *byte, uint32, *byte, *uint32, *uintptr, *uintptr) int32
	modifyItem            func(uintptr, uintptr, uint32, *byte) int32
	deleteItem            func(uintptr) int32
	freeContent           func(uintptr, uintptr) int32
	releaseCoreFoundation func(uintptr)
}

func NewOSStore() OSStore {
	return &darwinKeychainStore{}
}

func (s *darwinKeychainStore) load() {
	security, err := purego.Dlopen(
		"/System/Library/Frameworks/Security.framework/Security",
		purego.RTLD_LAZY|purego.RTLD_LOCAL,
	)
	if err != nil {
		s.err = err
		return
	}
	coreFoundation, err := purego.Dlopen(
		"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
		purego.RTLD_LAZY|purego.RTLD_LOCAL,
	)
	if err != nil {
		s.err = err
		return
	}
	purego.RegisterLibFunc(&s.addGenericPassword, security, "SecKeychainAddGenericPassword")
	purego.RegisterLibFunc(&s.findGenericPassword, security, "SecKeychainFindGenericPassword")
	purego.RegisterLibFunc(&s.modifyItem, security, "SecKeychainItemModifyAttributesAndData")
	purego.RegisterLibFunc(&s.deleteItem, security, "SecKeychainItemDelete")
	purego.RegisterLibFunc(&s.freeContent, security, "SecKeychainItemFreeContent")
	purego.RegisterLibFunc(&s.releaseCoreFoundation, coreFoundation, "CFRelease")
}

func (s *darwinKeychainStore) ready() bool {
	s.once.Do(s.load)
	return s.err == nil
}

func (s *darwinKeychainStore) Get(ctx context.Context, id string) ([]byte, error) {
	if err := validateOSSecretID(id); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.ready() {
		return nil, ErrOSStoreUnavailable
	}
	value, item, status := s.find(id)
	if item != 0 {
		s.releaseCoreFoundation(item)
	}
	if status == errSecItemNotFound {
		return nil, ErrOSSecretNotFound
	}
	if status != errSecSuccess {
		return nil, ErrOSStoreUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *darwinKeychainStore) Put(ctx context.Context, id, configKey string, value []byte) error {
	if err := validateOSSecretID(id); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.ready() {
		return ErrOSStoreUnavailable
	}
	label := "MisterMorph"
	if configKey = strings.TrimSpace(configKey); configKey != "" {
		label += " · " + configKey
	}
	labelBytes := []byte(label)
	attribute := secKeychainAttribute{tag: secLabelItemAttr, length: uint32(len(labelBytes)), data: uintptr(unsafe.Pointer(bytesPointer(labelBytes)))}
	attributes := secKeychainAttributeList{count: 1, attributes: uintptr(unsafe.Pointer(&attribute))}

	_, item, status := s.find(id)
	if status == errSecSuccess {
		defer s.releaseCoreFoundation(item)
		status = s.modifyItem(item, uintptr(unsafe.Pointer(&attributes)), uint32(len(value)), bytesPointer(value))
	} else if status == errSecItemNotFound {
		service := []byte(osKeyringService)
		account := []byte(id)
		status = s.addGenericPassword(
			0,
			uint32(len(service)), bytesPointer(service),
			uint32(len(account)), bytesPointer(account),
			uint32(len(value)), bytesPointer(value),
			&item,
		)
		if item != 0 {
			defer s.releaseCoreFoundation(item)
		}
		if status == errSecSuccess {
			status = s.modifyItem(item, uintptr(unsafe.Pointer(&attributes)), uint32(len(value)), bytesPointer(value))
		}
		runtime.KeepAlive(service)
		runtime.KeepAlive(account)
	}
	runtime.KeepAlive(value)
	runtime.KeepAlive(labelBytes)
	runtime.KeepAlive(attribute)
	runtime.KeepAlive(attributes)
	if status != errSecSuccess {
		return ErrOSStoreUnavailable
	}
	return ctx.Err()
}

func (s *darwinKeychainStore) Delete(ctx context.Context, id string) error {
	if err := validateOSSecretID(id); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.ready() {
		return ErrOSStoreUnavailable
	}
	_, item, status := s.find(id)
	if status == errSecItemNotFound {
		return ErrOSSecretNotFound
	}
	if status != errSecSuccess {
		return ErrOSStoreUnavailable
	}
	defer s.releaseCoreFoundation(item)
	if status = s.deleteItem(item); status != errSecSuccess {
		return ErrOSStoreUnavailable
	}
	return ctx.Err()
}

func (s *darwinKeychainStore) find(id string) ([]byte, uintptr, int32) {
	service := []byte(osKeyringService)
	account := []byte(id)
	var length uint32
	var data uintptr
	var item uintptr
	status := s.findGenericPassword(
		0,
		uint32(len(service)), bytesPointer(service),
		uint32(len(account)), bytesPointer(account),
		&length,
		&data,
		&item,
	)
	runtime.KeepAlive(service)
	runtime.KeepAlive(account)
	if status != errSecSuccess {
		return nil, item, status
	}
	value := append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(data)), int(length))...)
	_ = s.freeContent(0, data)
	return value, item, status
}

func bytesPointer(value []byte) *byte {
	if len(value) == 0 {
		return nil
	}
	return &value[0]
}
