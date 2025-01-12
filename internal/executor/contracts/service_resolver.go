package contracts

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/meshplus/bitxhub-core/boltvm"
	"github.com/meshplus/bitxhub-model/constant"
	"github.com/meshplus/bitxhub-model/pb"
)

type ServDomainType string

const (
	ReverseMap = "reverseMap"
)

type ServiceResolver struct {
	boltvm.Stub
}

type ServDomainData struct {
	Addr        map[uint64]string `json:"addr"`
	ServiceName string            `json:"serviceName"`
	Des         string            `json:"des"`
	Dids        []string          `json:"dids"`
}

func (sr ServiceResolver) SetServDomainData(name string, coinTyp uint64, addr string, serviceName string, des string, dids string) *boltvm.Response {
	/*if !checkBxhAddress(addr) {
		return boltvm.Error(boltvm.BnsErrCode, fmt.Sprintf("The address is not valid"))
	}*/
	if !sr.authorised(name) {
		return boltvm.Error(boltvm.BnsErrCode, "The domain name does not belong to you")
	}
	_addr := make(map[uint64]string)
	_addr[coinTyp] = addr
	didArr := strings.Split(dids, ",")
	servDomainData := ServDomainData{
		Addr:        _addr,
		ServiceName: serviceName,
		Des:         des,
		Dids:        didArr,
	}
	sr.SetObject(name, servDomainData)
	return boltvm.Success(nil)
}

func (sr ServiceResolver) GetServDomainData(name string) *boltvm.Response {
	if !sr.checkDomainAvailable(name) {
		return boltvm.Error(boltvm.BnsErrCode, "The domain id must be registered")
	}
	servDomainData := sr.getDataByDomain(name)
	servDomainBytes, err := json.Marshal(servDomainData)
	if err != nil {
		return boltvm.Error(boltvm.BnsErrCode, fmt.Sprintf("marshal servDomainData error: %v", err))
	}
	return boltvm.Success(servDomainBytes)
}

func (sr ServiceResolver) SetAddr(name string, coinTyp uint64, addr string) *boltvm.Response {
	if !sr.authorised(name) {
		return boltvm.Error(boltvm.BnsErrCode, "The domain name does not belong to you")
	}
	servDomainData := sr.getDataByDomain(name)
	servDomainData.Addr[coinTyp] = addr
	sr.SetObject(name, servDomainData)
	return boltvm.Success(nil)
}

func (sr ServiceResolver) SetServiceName(name string, serviceName string, reverse bool) *boltvm.Response {
	if !sr.authorised(name) {
		return boltvm.Error(boltvm.BnsErrCode, "The domain name does not belong to you")
	}
	if serviceName == "" {
		return boltvm.Error(boltvm.BnsErrCode, "The serviceName can not be an empty string")
	}
	servDomainData := sr.getDataByDomain(name)
	servDomainData.ServiceName = serviceName
	sr.SetObject(name, servDomainData)
	if reverse {
		sr.setReverseName(serviceName, name)
	}
	return boltvm.Success(nil)
}

func (sr ServiceResolver) SetServiceDes(name string, des string) *boltvm.Response {
	if !sr.authorised(name) {
		return boltvm.Error(boltvm.BnsErrCode, "The domain name does not belong to you")
	}
	servDomainData := sr.getDataByDomain(name)
	servDomainData.Des = des
	sr.SetObject(name, servDomainData)
	return boltvm.Success(nil)
}

func (sr ServiceResolver) SetDids(name string, dids string) *boltvm.Response {
	if !sr.authorised(name) {
		return boltvm.Error(boltvm.BnsErrCode, "The domain name does not belong to you")
	}
	didArr := strings.Split(dids, ",")
	servDomainData := sr.getDataByDomain(name)
	servDomainData.Dids = didArr
	sr.SetObject(name, servDomainData)
	return boltvm.Success(nil)
}

func (sr ServiceResolver) SetReverse(serviceName string, name string) *boltvm.Response {
	if !sr.authorised(name) {
		return boltvm.Error(boltvm.BnsErrCode, "The domain name does not belong to you")
	}
	sr.setReverseName(serviceName, name)
	return boltvm.Success(nil)
}

func (sr ServiceResolver) GetReverseName(serviceName string) *boltvm.Response {
	reverseName := make(map[string][]string)
	ok := sr.GetObject(ReverseMap, &reverseName)
	if !ok {
		return boltvm.Error(boltvm.BnsErrCode, "there is not exist key")
	}
	reverseNameBytes, err := json.Marshal(reverseName[serviceName])
	if err != nil {
		return boltvm.Error(boltvm.BnsErrCode, fmt.Sprintf("marshal servDomainData error: %v", err))
	}
	return boltvm.Success(reverseNameBytes)
}

func (sr ServiceResolver) GetServiceName(name string) *boltvm.Response {
	if !sr.checkDomainAvailable(name) {
		return boltvm.Error(boltvm.BnsErrCode, "The domain id must be registered")
	}
	servDomainData := sr.getDataByDomain(name)
	return boltvm.Success([]byte(servDomainData.ServiceName))
}

func (sr ServiceResolver) DeleteServDomainData(name string) *boltvm.Response {
	if !sr.authorised(name) {
		return boltvm.Error(boltvm.BnsErrCode, "The domain name does not belong to you")
	}
	nameArr := strings.Split(name, ".")
	if len(nameArr) != 3 {
		return boltvm.Error(boltvm.BnsErrCode, "The domain name must be second")
	}
	servDomainData := sr.getDataByDomain(name)
	serviceName := servDomainData.ServiceName
	sr.Delete(name)
	reverseName := make(map[string][]string)
	sr.GetObject(ReverseMap, &reverseName)
	reverseNameArr := reverseName[serviceName]
	index := -1
	for i, v := range reverseNameArr {
		if v == name {
			index = i
			break
		}
	}
	if index != -1 {
		reverseNameArr = append(reverseNameArr[:index], reverseNameArr[(index+1):]...)
	}
	sr.SetObject(ReverseMap, reverseName)
	return boltvm.Success(nil)
}

func (sr ServiceResolver) getDataByDomain(name string) ServDomainData {
	servDomainData := ServDomainData{}
	sr.GetObject(name, &servDomainData)
	return servDomainData
}

func (sr ServiceResolver) setReverseName(serviceName string, name string) {
	reverseName := make(map[string][]string)
	sr.GetObject(ReverseMap, &reverseName)
	reverseNameArr := reverseName[serviceName]
	exist := isContain(reverseNameArr, name)
	if !exist {
		reverseNameArr = append(reverseNameArr, name)
	}
	reverseName[serviceName] = reverseNameArr
	sr.SetObject(ReverseMap, reverseName)
}

func (sr ServiceResolver) authorised(name string) bool {
	res := sr.CrossInvoke(constant.ServiceRegistryContractAddr.Address().String(), "Owner",
		pb.String(name))
	if !res.Ok {
		return false
	}
	owner := string(res.Result)

	currentCaller := sr.CurrentCaller()

	res = sr.CrossInvoke(constant.ServiceRegistryContractAddr.Address().String(), "IsApproved",
		pb.String(currentCaller), pb.String(string(constant.ServiceResolverContractAddr)))
	if !res.Ok {
		return false
	}
	isApprove, err := strconv.ParseBool(string(res.Result))
	if err != nil {
		return false
	}
	caller := sr.Caller()
	return owner == caller || isApprove
}

func (sr ServiceResolver) checkDomainAvailable(name string) bool {
	res := sr.CrossInvoke(constant.ServiceRegistryContractAddr.Address().String(), "Owner",
		pb.String(name))
	return res.Ok
}

func isContain(items []string, item string) bool {
	for _, eachItem := range items {
		if eachItem == item {
			return true
		}
	}
	return false
}
