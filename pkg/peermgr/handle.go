package peermgr

import (
	"fmt"
	"strconv"
	"strings"

	orderPeerMgr "github.com/meshplus/bitxhub-core/peer-mgr"
	"github.com/meshplus/bitxhub-model/pb"
	"github.com/meshplus/bitxhub/internal/model"
	"github.com/meshplus/bitxhub/pkg/utils"
	network "github.com/meshplus/go-lightp2p"
	"github.com/sirupsen/logrus"
)

func (swarm *Swarm) handleMessage(s network.Stream, data []byte) {
	m := &pb.Message{}
	if err := m.Unmarshal(data); err != nil {
		swarm.logger.Errorf("unmarshal message error: %s", err.Error())
		return
	}

	handler := func() error {
		if m.Type != pb.Message_CONSENSUS {
			swarm.logger.Debugf("handle msg: %s", m.Type)
		}
		switch m.Type {
		case pb.Message_GET_BLOCK:
			return swarm.handleGetBlockPack(s, m)
		case pb.Message_GET_BLOCK_HEADERS:
			return swarm.handleGetBlockHeadersPack(s, m)
		case pb.Message_GET_BLOCKS:
			return swarm.handleGetBlocksPack(s, m)
		case pb.Message_FETCH_CERT:
			return swarm.handleFetchCertMessage(s)
		case pb.Message_FETCH_P2P_PUBKEY:
			return swarm.handleFetchP2PPubkey(s)
		case pb.Message_FETCH_TSS_PUBKEY:
			go swarm.handleFetchTssPubKey(s)
		case pb.Message_FETCH_TSS_INFO:
			go swarm.handleFetchTssInfo(s)
		case pb.Message_CONSENSUS:
			go swarm.orderMessageFeed.Send(orderPeerMgr.OrderMessageEvent{Data: m.Data})
		case pb.Message_FETCH_BLOCK_SIGN:
			go swarm.handleFetchBlockSignMessage(s, m.Data)
		case pb.Message_FETCH_IBTP_REQUEST_SIGN:
			go swarm.handleFetchIBTPSignMessage(s, m.Data, true)
		case pb.Message_FETCH_IBTP_RESPONSE_SIGN:
			go swarm.handleFetchIBTPSignMessage(s, m.Data, false)
		case pb.Message_FETCH_IBTP_REQUEST_TSS_SIGN:
			go swarm.handleFetchIBTPTssSignMessage(s, m.Data, true)
		case pb.Message_FETCH_IBTP_RESPONSE_TSS_SIGN:
			go swarm.handleFetchIBTPTssSignMessage(s, m.Data, false)
		case pb.Message_FETCH_IBTP_TSS_SIGN_ACK:
			go swarm.tssSignResultFeed.Send(m)
		case pb.Message_CHECK_MASTER_PIER:
			swarm.handleAskPierMaster(s, m.Data)
		case pb.Message_CHECK_MASTER_PIER_ACK:
			swarm.handleReplyPierMaster(s, m.Data)
		case pb.Message_TSS_TASK, pb.Message_FETCH_TSS_NODES:
			go swarm.tssMessageFeed.Send(m)
		case pb.Message_TSS_CULPRITS:
			swarm.logger.Debugf("handle tss culprits msg")
			go swarm.tssCulpritsFeed.Send(m)
		case pb.Message_Tss_KEYSIGN_NOT_PARTIES:
			go swarm.handleNotTssParties(s, m.Data)
		default:
			swarm.logger.WithField("module", "p2p").Errorf("can't handle msg[type: %v]", m.Type)
			return nil
		}

		return nil
	}

	go func() {
		if err := handler(); err != nil {
			swarm.logger.WithFields(logrus.Fields{
				"error": err,
				"type":  m.Type.String(),
			}).Error("Handle message")
		}
	}()
}

func (swarm *Swarm) handleGetBlockPack(s network.Stream, msg *pb.Message) error {
	num, err := strconv.ParseUint(string(msg.Data), 10, 64)
	if err != nil {
		return fmt.Errorf("convert %s string to int failed: %w", string(msg.Data), err)
	}

	block, err := swarm.ledger.GetBlock(num, true)
	if err != nil {
		return fmt.Errorf("get block with height %d failed: %w", num, err)
	}

	v, err := block.Marshal()
	if err != nil {
		return fmt.Errorf("marshal block error: %w", err)
	}

	m := &pb.Message{
		Type: pb.Message_GET_BLOCK_ACK,
		Data: v,
	}

	if err := swarm.SendWithStream(s, m); err != nil {
		return fmt.Errorf("send %s with stream failed: %w", m.String(), err)
	}

	return nil
}

func (swarm *Swarm) handleGetBlockHeadersPack(s network.Stream, msg *pb.Message) error {
	req := &pb.GetBlockHeadersRequest{}
	if err := req.Unmarshal(msg.Data); err != nil {
		return fmt.Errorf("unmarshal get block headers request error: %w", err)
	}

	res := &pb.GetBlockHeadersResponse{}
	blockHeaders := make([]*pb.BlockHeader, 0)
	for i := req.Start; i <= req.End; i++ {
		block, err := swarm.ledger.GetBlock(i, false)
		if err != nil {
			return fmt.Errorf("get block with height %d from ledger failed: %w", i, err)
		}
		blockHeaders = append(blockHeaders, block.BlockHeader)
	}
	res.BlockHeaders = blockHeaders
	v, err := res.Marshal()
	if err != nil {
		return fmt.Errorf("marshal get block headers response error: %w", err)
	}
	m := &pb.Message{
		Type: pb.Message_GET_BLOCK_HEADERS_ACK,
		Data: v,
	}

	if err := swarm.SendWithStream(s, m); err != nil {
		return fmt.Errorf("send %s with stream failed: %w", m.String(), err)
	}

	return nil
}

func (swarm *Swarm) handleFetchCertMessage(s network.Stream) error {
	certs := &model.CertsMessage{
		AgencyCert: swarm.repo.Certs.AgencyCertData,
		NodeCert:   swarm.repo.Certs.NodeCertData,
	}

	data, err := certs.Marshal()
	if err != nil {
		return fmt.Errorf("marshal certs: %w", err)
	}

	msg := &pb.Message{
		Type: pb.Message_FETCH_CERT_ACK,
		Data: data,
	}

	err = swarm.SendWithStream(s, msg)
	if err != nil {
		return fmt.Errorf("send msg: %w", err)
	}

	return nil
}

func (swarm *Swarm) handleFetchP2PPubkey(s network.Stream) error {
	if swarm.repo.Config.Tss.EnableTSS {
		pubkeyData, err := swarm.p2p.PrivKey().GetPublic().Raw()
		if err != nil {
			return fmt.Errorf("get p2p pubkey data error: %w", err)
		}

		msg := &pb.Message{
			Type: pb.Message_FETCH_P2P_PUBKEY_ACK,
			Data: pubkeyData,
		}

		err = swarm.SendWithStream(s, msg)
		if err != nil {
			return fmt.Errorf("send msg: %w", err)
		}
	}
	return nil
}

func (swarm *Swarm) handleFetchTssPubKey(s network.Stream) {
	addr, _, err := swarm.Tss.GetTssPubkey()
	if err != nil {
		swarm.logger.Errorf("we do not have tss pubkey info: %v", err)
		return
	}

	msg := &pb.Message{
		Type: pb.Message_FETCH_TSS_PUBKEY_ACK,
		Data: []byte(addr),
	}

	err = swarm.SendWithStream(s, msg)
	if err != nil {
		swarm.logger.Errorf("send msg: %w", err)
		return
	}
}

func (swarm *Swarm) handleFetchTssInfo(s network.Stream) {
	info, err := swarm.Tss.GetTssInfo()
	if err != nil {
		swarm.logger.Errorf("we do not know tss keygen parties pk: %v", err)
		return
	}

	data, err := info.Marshal()
	if err != nil {
		swarm.logger.Errorf("tss info marshal error: %v", err)
		return
	}
	msg := &pb.Message{
		Type: pb.Message_FETCH_TSS_INFO_ACK,
		Data: data,
	}

	err = swarm.SendWithStream(s, msg)
	if err != nil {
		swarm.logger.Errorf("send msg: %w", err)
		return
	}
}

func (swarm *Swarm) handleFetchBlockSignMessage(s network.Stream, data []byte) {
	handle := func(data []byte) ([]byte, error) {
		height, err := strconv.ParseUint(string(data), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse height: %w", err)
		}

		swarm.logger.WithField("height", height).Debug("Handle fetching block sign message")

		signed, err := swarm.ledger.GetBlockSign(height)
		if err != nil {
			return nil, fmt.Errorf("get block sign: %w", err)
		}

		return signed, nil
	}

	signed, err := handle(data)
	if err != nil {
		swarm.logger.Errorf("handle fetch-block-sign: %s", err)
		return
	}

	m := model.MerkleWrapperSign{
		Address:   swarm.repo.Key.Address,
		Signature: signed,
	}

	body, err := m.Marshal()
	if err != nil {
		swarm.logger.Errorf("marshal merkle wrapper sign: %s", err.Error())
		return
	}

	msg := &pb.Message{
		Type: pb.Message_FETCH_BLOCK_SIGN_ACK,
		Data: body,
	}

	if err := swarm.SendWithStream(s, msg); err != nil {
		swarm.logger.Errorf("send block sign back: %s", err.Error())
	}
}

func (swarm *Swarm) handleFetchAssetExchangeSignMessage(s network.Stream, data []byte) {
}

func (swarm *Swarm) handleFetchIBTPSignMessage(s network.Stream, data []byte, isReq bool) {
	address, signed, err := utils.GetIBTPSign(swarm.ledger, string(data), isReq, swarm.repo.Key.PrivKey)
	if err != nil {
		swarm.logger.Errorf("handle fetch-ibtp-sign for ibtp %s isReq %v: %s", string(data), isReq, err.Error())
		return
	}

	m := model.MerkleWrapperSign{
		Address:   address,
		Signature: signed,
	}

	body, err := m.Marshal()
	if err != nil {
		swarm.logger.Errorf("marshal merkle wrapper sign: %s", err.Error())
		return
	}

	msg := &pb.Message{
		Type: pb.Message_FETCH_IBTP_SIGN_ACK,
		Data: body,
	}

	if err := swarm.SendWithStream(s, msg); err != nil {
		swarm.logger.Errorf("send asset exchange sign back: %s", err.Error())
	}
}

func (swarm *Swarm) handleNotTssParties(s network.Stream, data []byte) {
	req := &pb.GetSignsRequest{}
	if err := req.Unmarshal(data); err != nil {
		swarm.logger.Errorf("handle fetch-ibtp-tss-sign unmarshal req error: %v", err)
		//ackErr = fmt.Errorf("handle fetch-ibtp-tss-sign unmarshal req error: %v", err).Error()
		//swarm.wrapperSendMsg(s, msgType, []byte(ackErr))
		return
	}
	var isReq bool
	if req.Type == pb.GetSignsRequest_TSS_IBTP_REQUEST {
		isReq = true
	}

	signInfo := strings.Split(string(req.Extra), "-")
	swarm.logger.Debugf("handleNotTssParties, notParties nodes: %s", signInfo[2])
	err := utils.NotifyNotTssParties(swarm.Tss, swarm.ledger, req.Content, isReq, strings.Split(signInfo[0], ","), signInfo[1])
	if err != nil {
		swarm.logger.Errorf("handle not parties tss nodes keyRoundDone err: ibtp: %s isReq %v: %s", string(data), isReq, err.Error())
		return
	}

	msg := &pb.Message{
		Type: pb.Message_Tss_KEYSIGN_NOT_PARTIES_ACK,
		Data: []byte("ok"),
	}

	if err := swarm.SendWithStream(s, msg); err != nil {
		swarm.logger.Errorf("send tss back: %s", err.Error())
	}
}

//
//func (swarm *Swarm) wrapperSendMsg(s network.Stream, typ pb.Message_Type, data []byte) error {
//	msg := &pb.Message{
//		Type: typ,
//		Data: data,
//	}
//	if err := swarm.SendWithStream(s, msg); err != nil {
//		swarm.logger.Errorf("send msg with stream err: %s", err.Error())
//		return err
//	}
//	return nil
//}

func (swarm *Swarm) handleFetchIBTPTssSignMessage(s network.Stream, data []byte, isReq bool) {
	req := &pb.GetSignsRequest{}
	if err := req.Unmarshal(data); err != nil {
		swarm.logger.Errorf("handle fetch-ibtp-tss-sign unmarshal req error: %v", err)
		return
	}

	signInfo := strings.Split(string(req.Extra), "-")

	swarm.logger.Debugf("handleFetchIBTPTssSignMessage, signers: %s", signInfo[0])
	signed, culpritIDs, err := utils.GetIBTPTssSign(swarm.Tss, swarm.ledger, req.Content, isReq, strings.Split(signInfo[0], ","), signInfo[1])
	if err != nil {
		swarm.logger.Errorf("handle fetch-ibtp-tss-sign for ibtp: %s isReq %v: %s", string(data), isReq, err.Error())
		return
	}

	m := model.MerkleWrapperSign{
		Address:    swarm.repo.Key.Address,
		Signature:  signed,
		CulpritIDs: culpritIDs,
	}

	body, err := m.Marshal()
	if err != nil {
		swarm.logger.Errorf("marshal merkle wrapper sign: %s", err.Error())
		return
	}

	msg := &pb.Message{
		Type: pb.Message_FETCH_IBTP_TSS_SIGN_ACK,
		Data: body,
	}

	if err := swarm.Broadcast(msg); err != nil {
		swarm.logger.Errorf("broadcast tss sign back: %s", err.Error())
	}
}

func (swarm *Swarm) handleGetBlocksPack(s network.Stream, msg *pb.Message) error {
	req := &pb.GetBlocksRequest{}
	if err := req.Unmarshal(msg.Data); err != nil {
		return fmt.Errorf("unmarshal get blcoks request error: %w", err)
	}

	res := &pb.GetBlocksResponse{}
	blocks := make([]*pb.Block, 0)
	for i := req.Start; i <= req.End; i++ {
		block, err := swarm.ledger.GetBlock(i, true)
		if err != nil {
			return fmt.Errorf("get block with height %d from ledger failed: %w", i, err)
		}
		blocks = append(blocks, block)
	}
	res.Blocks = blocks
	v, err := res.Marshal()
	if err != nil {
		return fmt.Errorf("marshal get blocks response error: %w", err)
	}
	m := &pb.Message{
		Type: pb.Message_GET_BLOCKS_ACK,
		Data: v,
	}

	if err := swarm.SendWithStream(s, m); err != nil {
		return fmt.Errorf("send %s with stream failed: %w", m.String(), err)
	}

	return nil
}

func (swarm *Swarm) handleAskPierMaster(s network.Stream, data []byte) {
	address := string(data)
	resp := &pb.CheckPierResponse{}
	if swarm.piers.pierChan.checkAddress(address) {
		resp.Status = pb.CheckPierResponse_HAS_MASTER
	} else {
		if !swarm.piers.pierMap.hasPier(address) {
			return
		}
		if swarm.piers.pierMap.checkMaster(address) {
			resp.Status = pb.CheckPierResponse_HAS_MASTER
		} else {
			swarm.piers.pierMap.rmMaster(address)
			return
		}
	}
	resp.Address = address
	msgData, err := resp.Marshal()
	if err != nil {
		swarm.logger.Errorf("marshal ask pier master response: %s", err.Error())
		return
	}
	message := &pb.Message{
		Data: msgData,
		Type: pb.Message_CHECK_MASTER_PIER_ACK,
	}
	msg, err := message.Marshal()
	if err != nil {
		swarm.logger.Errorf("marshal response message: %s", err.Error())
		return
	}

	if err := swarm.p2p.AsyncSend(s.RemotePeerID(), msg); err != nil {
		swarm.logger.Errorf("send response: %s", err.Error())
		return
	}
}

func (swarm *Swarm) handleReplyPierMaster(s network.Stream, data []byte) {
	resp := &pb.CheckPierResponse{}
	err := resp.Unmarshal(data)
	if err != nil {
		swarm.logger.Errorf("unmarshal response: %s", err.Error())
		return
	}
	swarm.piers.pierChan.writeChan(resp)
}

//func (swarm *Swarm) handleSupportTss(s network.Stream) {
//	var msg *pb.Message
//	if swarm.repo.Config.Tss.EnableTSS {
//		msg = &pb.Message{
//			Data: []byte(pb.SignType_TSS.String()),
//		}
//	} else {
//		msg = &pb.Message{
//			Data: []byte(pb.SignType_MultiSign.String()),
//		}
//	}
//	err := swarm.SendWithStream(s, msg)
//	if err != nil {
//		return
//	}
//}
