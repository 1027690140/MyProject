package consumer

import (
	"errors"
	"strings"
)

type Service struct {
	Class  string
	Method string
	AppId  string
	Addrs  []string // demo http@10.0.0.1:2222; tcp@10.0.0.1:1111
}

// demo: User.GetUserById
// demo: User.GetUserById.15;http@10.0.0.1:2222;tcp@10.0.0.1:1111
func NewService(servicePath string) (*Service, error) {
	arr := strings.Split(servicePath, ".")
	service := &Service{}
	if len(arr) < 2 {
		return service, errors.New("service path inlegal")
	}
	service.Class = arr[0]
	service.Method = arr[1]
	if len(arr) >= 3 {
		service.AppId = arr[3]
	}
	if len(arr) >= 4 {
		addr := strings.Split(servicePath, ";")
		service.Addrs = append(service.Addrs, addr[3:]...)
	}
	return service, nil
}

func (service *Service) SetAddr(addrs ...string) error {
	if len(addrs) == 0 {
		return errors.New("without addrs")
	}
	service.Addrs = addrs
	return nil
}

func (service *Service) GetAddr() ([]string, error) {
	return service.Addrs, nil
}
