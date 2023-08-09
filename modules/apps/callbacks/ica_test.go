package ibccallbacks_test

import (
	"fmt"
	"time"

	"github.com/cosmos/gogoproto/proto"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	icacontrollertypes "github.com/cosmos/ibc-go/v7/modules/apps/27-interchain-accounts/controller/types"
	icahosttypes "github.com/cosmos/ibc-go/v7/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v7/modules/apps/27-interchain-accounts/types"
	"github.com/cosmos/ibc-go/v7/modules/apps/callbacks/types"
	ibctesting "github.com/cosmos/ibc-go/v7/testing"
)

func (s *CallbacksTestSuite) TestICACallbacks() {
	// Destination callbacks are not supported for ICA packets
	testCases := []struct {
		name        string
		icaMemo     string
		expCallback types.CallbackTrigger
		expSuccess  bool
	}{
		{
			"success: transfer with no memo",
			"",
			"none",
			true,
		},
		{
			"success: dest callback",
			fmt.Sprintf(`{"dest_callback": {"address": "%s"}}`, callbackAddr),
			"none",
			true,
		},
		{
			"success: dest callback with other json fields",
			fmt.Sprintf(`{"dest_callback": {"address": "%s"}, "something_else": {}}`, callbackAddr),
			"none",
			true,
		},
		{
			"success: dest callback with malformed json",
			fmt.Sprintf(`{"dest_callback": {"address": "%s"}, malformed}`, callbackAddr),
			"none",
			true,
		},
		{
			"success: dest callback with missing address",
			`{"dest_callback": {"address": ""}}`,
			"none",
			true,
		},
		{
			"success: source callback",
			fmt.Sprintf(`{"src_callback": {"address": "%s"}}`, callbackAddr),
			types.CallbackTriggerAcknowledgementPacket,
			true,
		},
		{
			"success: source callback with other json fields",
			fmt.Sprintf(`{"src_callback": {"address": "%s"}, "something_else": {}}`, callbackAddr),
			types.CallbackTriggerAcknowledgementPacket,
			true,
		},
		{
			"success: source callback with malformed json",
			fmt.Sprintf(`{"src_callback": {"address": "%s"}, malformed}`, callbackAddr),
			"none",
			true,
		},
		{
			"success: source callback with missing address",
			`{"src_callback": {"address": ""}}`,
			"none",
			true,
		},
		{
			"failure: dest callback with low gas (panic)",
			fmt.Sprintf(`{"dest_callback": {"address": "%s", "gas_limit": "350000"}}`, callbackAddr),
			"none",
			false,
		},
		{
			"failure: source callback with low gas (panic)",
			fmt.Sprintf(`{"src_callback": {"address": "%s", "gas_limit": "350000"}}`, callbackAddr),
			types.CallbackTriggerAcknowledgementPacket,
			false,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			icaAddr := s.SetupICATest()

			s.ExecuteICATx(icaAddr, tc.icaMemo, 1)
			s.AssertHasExecutedExpectedCallback(tc.expCallback, tc.expSuccess)
		})
	}
}

func (s *CallbacksTestSuite) TestICATimeoutCallbacks() {
	// ICA channels are closed after a timeout packet is executed
	testCases := []struct {
		name        string
		icaMemo     string
		expCallback types.CallbackTrigger
		expSuccess  bool
	}{
		{
			"success: transfer with no memo",
			"",
			"none",
			true,
		},
		{
			"success: dest callback",
			fmt.Sprintf(`{"dest_callback": {"address": "%s"}}`, callbackAddr),
			"none",
			true,
		},
		{
			"success: source callback",
			fmt.Sprintf(`{"src_callback": {"address": "%s"}}`, callbackAddr),
			types.CallbackTriggerTimeoutPacket,
			true,
		},
		{
			"success: dest callback with low gas (panic)",
			fmt.Sprintf(`{"dest_callback": {"address": "%s", "gas_limit": "350000"}}`, callbackAddr),
			"none",
			true,
		},
		{
			"failure: source callback with low gas (panic)",
			fmt.Sprintf(`{"src_callback": {"address": "%s", "gas_limit": "350000"}}`, callbackAddr),
			types.CallbackTriggerTimeoutPacket,
			false,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			icaAddr := s.SetupICATest()

			s.ExecuteICATimeout(icaAddr, tc.icaMemo, 1)
			s.AssertHasExecutedExpectedCallback(tc.expCallback, tc.expSuccess)
		})
	}
}

// ExecuteICATx executes a stakingtypes.MsgDelegate on chainB by sending a packet containing the msg to chainB
func (s *CallbacksTestSuite) ExecuteICATx(icaAddress, memo string, seq uint64) {
	timeoutTimestamp := uint64(s.chainA.GetContext().BlockTime().Add(time.Minute).UnixNano())
	icaOwner := s.chainA.SenderAccount.GetAddress().String()
	connectionID := s.path.EndpointA.ConnectionID
	// build the interchain accounts packet data
	packetData := s.buildICAMsgDelegatePacketData(icaAddress, memo)
	msg := icacontrollertypes.NewMsgSendTx(icaOwner, connectionID, timeoutTimestamp, packetData)

	res, err := s.chainA.SendMsgs(msg)
	s.Require().NoError(err) // message committed
	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents().ToABCIEvents())
	s.Require().NoError(err)

	err = s.path.RelayPacket(packet)
	s.Require().NoError(err)
}

// ExecuteICATx executes a stakingtypes.MsgDelegate on chainB by sending a packet containing the msg to chainB
func (s *CallbacksTestSuite) ExecuteICATimeout(icaAddress, memo string, seq uint64) {
	timeoutTimestamp := uint64(s.chainB.GetContext().BlockTime().UnixNano())
	icaOwner := s.chainA.SenderAccount.GetAddress().String()
	connectionID := s.path.EndpointA.ConnectionID
	// build the interchain accounts packet data
	packetData := s.buildICAMsgDelegatePacketData(icaAddress, memo)
	msg := icacontrollertypes.NewMsgSendTx(icaOwner, connectionID, timeoutTimestamp, packetData)

	res, err := s.chainA.SendMsgs(msg)
	s.Require().NoError(err) // message committed
	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents().ToABCIEvents())
	s.Require().NoError(err)

	module, _, err := s.chainA.App.GetIBCKeeper().PortKeeper.LookupModuleByPort(s.chainA.GetContext(), s.path.EndpointA.ChannelConfig.PortID)
	s.Require().NoError(err)

	cbs, ok := s.chainA.App.GetIBCKeeper().Router.GetRoute(module)
	s.Require().True(ok)

	err = cbs.OnTimeoutPacket(s.chainA.GetContext(), packet, nil)
	s.Require().NoError(err)
}

// buildICAMsgDelegatePacketData builds a packetData containing a stakingtypes.MsgDelegate to be executed on chainB
func (s *CallbacksTestSuite) buildICAMsgDelegatePacketData(icaAddress string, memo string) icatypes.InterchainAccountPacketData {
	// prepare a simple stakingtypes.MsgDelegate to be used as the interchain account msg executed on chainB
	validatorAddr := (sdk.ValAddress)(s.chainB.Vals.Validators[0].Address)
	msgDelegate := &stakingtypes.MsgDelegate{
		DelegatorAddress: icaAddress,
		ValidatorAddress: validatorAddr.String(),
		Amount:           sdk.NewCoin(sdk.DefaultBondDenom, sdkmath.NewInt(5000)),
	}

	// ensure chainB is allowed to execute stakingtypes.MsgDelegate
	params := icahosttypes.NewParams(true, []string{sdk.MsgTypeURL(msgDelegate)})
	s.chainB.GetSimApp().ICAHostKeeper.SetParams(s.chainB.GetContext(), params)

	data, err := icatypes.SerializeCosmosTx(s.chainA.GetSimApp().AppCodec(), []proto.Message{msgDelegate}, icatypes.EncodingProtobuf)
	s.Require().NoError(err)

	icaPacketData := icatypes.InterchainAccountPacketData{
		Type: icatypes.EXECUTE_TX,
		Data: data,
		Memo: memo,
	}

	return icaPacketData
}
