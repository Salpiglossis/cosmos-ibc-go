package keeper_test

import (
	"encoding/hex"
	"errors"
	"fmt"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	clienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v6/modules/core/03-connection/types"
	"github.com/cosmos/ibc-go/v6/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v6/modules/core/24-host"
	"github.com/cosmos/ibc-go/v6/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v6/modules/light-clients/07-tendermint"
	ibctesting "github.com/cosmos/ibc-go/v6/testing"
	ibcmock "github.com/cosmos/ibc-go/v6/testing/mock"
)

var (
	disabledTimeoutTimestamp = uint64(0)
	disabledTimeoutHeight    = clienttypes.ZeroHeight()
	defaultTimeoutHeight     = clienttypes.NewHeight(1, 100)

	// for when the testing package cannot be used
	connIDA = "connA"
	connIDB = "connB"
)

// TestSendPacket tests SendPacket from chainA to chainB
func (suite *KeeperTestSuite) TestSendPacket() {
	var (
		path             *ibctesting.Path
		sourcePort       string
		sourceChannel    string
		packetData       []byte
		timeoutHeight    clienttypes.Height
		timeoutTimestamp uint64
		channelCap       *capabilitytypes.Capability
		hasEvents        bool
	)

	testCases := []testCase{
		{"success: UNORDERED channel", func() {
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			hasEvents = true
		}, true},
		{"success: ORDERED channel", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			hasEvents = true
		}, true},
		{"success with solomachine: UNORDERED channel", func() {
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			// swap client with solo machine
			solomachine := ibctesting.NewSolomachine(suite.T(), suite.chainA.Codec, "solomachinesingle", "testing", 1)
			path.EndpointA.ClientID = clienttypes.FormatClientIdentifier(exported.Solomachine, 10)
			path.EndpointA.SetClientState(solomachine.ClientState())
			connection := path.EndpointA.GetConnection()
			connection.ClientId = path.EndpointA.ClientID
			path.EndpointA.SetConnection(connection)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			hasEvents = true
		}, true},
		{"success with solomachine: ORDERED channel", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			// swap client with solomachine
			solomachine := ibctesting.NewSolomachine(suite.T(), suite.chainA.Codec, "solomachinesingle", "testing", 1)
			path.EndpointA.ClientID = clienttypes.FormatClientIdentifier(exported.Solomachine, 10)
			path.EndpointA.SetClientState(solomachine.ClientState())
			connection := path.EndpointA.GetConnection()
			connection.ClientId = path.EndpointA.ClientID
			path.EndpointA.SetConnection(connection)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			hasEvents = true
		}, true},
		{"packet basic validation failed, empty packet data", func() {
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			packetData = []byte{}
		}, false},
		{"channel not found", func() {
			// use wrong channel naming
			suite.coordinator.Setup(path)
			sourceChannel = ibctesting.InvalidID
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"channel closed", func() {
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			err := path.EndpointA.SetChannelClosed()
			suite.Require().NoError(err)
		}, false},
		{"connection not found", func() {
			// pass channel check
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			channel := path.EndpointA.GetChannel()
			channel.ConnectionHops[0] = "invalid-connection"
			path.EndpointA.SetChannel(channel)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"client state not found", func() {
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			// change connection client ID
			connection := path.EndpointA.GetConnection()
			connection.ClientId = ibctesting.InvalidID
			suite.chainA.App.GetIBCKeeper().ConnectionKeeper.SetConnection(suite.chainA.GetContext(), path.EndpointA.ConnectionID, connection)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"client state is frozen", func() {
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			connection := path.EndpointA.GetConnection()
			clientState := path.EndpointA.GetClientState()
			cs, ok := clientState.(*ibctm.ClientState)
			suite.Require().True(ok)

			// freeze client
			cs.FrozenHeight = clienttypes.NewHeight(0, 1)
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), connection.ClientId, cs)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},

		{"timeout height passed", func() {
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			// use client state latest height for timeout
			clientState := path.EndpointA.GetClientState()
			timeoutHeight = clientState.GetLatestHeight().(clienttypes.Height)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"timeout timestamp passed", func() {
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			// use latest time on client state
			clientState := path.EndpointA.GetClientState()
			connection := path.EndpointA.GetConnection()
			timestamp, err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.GetTimestampAtHeight(suite.chainA.GetContext(), connection, clientState.GetLatestHeight())
			suite.Require().NoError(err)

			timeoutHeight = disabledTimeoutHeight
			timeoutTimestamp = timestamp
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"timeout timestamp passed with solomachine", func() {
			suite.coordinator.Setup(path)
			// swap client with solomachine
			solomachine := ibctesting.NewSolomachine(suite.T(), suite.chainA.Codec, "solomachinesingle", "testing", 1)
			path.EndpointA.ClientID = clienttypes.FormatClientIdentifier(exported.Solomachine, 10)
			path.EndpointA.SetClientState(solomachine.ClientState())
			connection := path.EndpointA.GetConnection()
			connection.ClientId = path.EndpointA.ClientID
			path.EndpointA.SetConnection(connection)

			clientState := path.EndpointA.GetClientState()
			timestamp, err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.GetTimestampAtHeight(suite.chainA.GetContext(), connection, clientState.GetLatestHeight())
			suite.Require().NoError(err)

			sourceChannel = path.EndpointA.ChannelID
			timeoutHeight = disabledTimeoutHeight
			timeoutTimestamp = timestamp

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"next sequence send not found", func() {
			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			sourceChannel = path.EndpointA.ChannelID

			suite.coordinator.SetupConnections(path)
			// manually creating channel prevents next sequence from being set
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainA.GetContext(),
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID), []string{path.EndpointA.ConnectionID}, path.EndpointA.ChannelConfig.Version),
			)
			suite.chainA.CreateChannelCapability(suite.chainA.GetSimApp().ScopedIBCMockKeeper, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"channel capability not found", func() {
			suite.coordinator.Setup(path)
			sourceChannel = path.EndpointA.ChannelID

			channelCap = capabilitytypes.NewCapability(5)
		}, false},
	}

	for i, tc := range testCases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s, %d/%d tests", tc.msg, i, len(testCases)), func() {
			suite.SetupTest() // reset
			hasEvents = false // reset events flag
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			// set default send packet arguments
			// sourceChannel is set after path is setup
			sourcePort = path.EndpointA.ChannelConfig.PortID
			timeoutHeight = defaultTimeoutHeight
			timeoutTimestamp = disabledTimeoutTimestamp
			packetData = ibctesting.MockPacketData

			// malleate may modify send packet arguments above
			tc.malleate()

			ctx := suite.chainA.GetContext()
			// only check if nextSequenceSend exists in no error case since it is a tested error case above.
			expectedSequence, ok := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceSend(ctx, sourcePort, sourceChannel)

			sequence, err := suite.chainA.App.GetIBCKeeper().ChannelKeeper.SendPacket(ctx, channelCap,
				sourcePort, sourceChannel, timeoutHeight, timeoutTimestamp, packetData)

			if tc.expPass {
				suite.Require().NoError(err)
				// verify that the returned sequence matches expected value
				suite.Require().True(ok)
				suite.Require().Equal(expectedSequence, sequence, "send packet did not return the expected sequence of the outgoing packet")
			} else {
				suite.Require().Error(err)
			}

			// Verify events
			events := ctx.EventManager().Events()
			expEvents := map[string]map[string]string{
				"send_packet": {
					"packet_data":              string(packetData),
					"packet_data_hex":          hex.EncodeToString(packetData),
					"packet_timeout_height":    timeoutHeight.String(),
					"packet_timeout_timestamp": fmt.Sprintf("%d", timeoutTimestamp),
					"packet_sequence":          fmt.Sprintf("%d", expectedSequence),
					"packet_src_port":          sourcePort,
					"packet_src_channel":       sourceChannel,
					"packet_dst_port":          path.EndpointB.ChannelConfig.PortID,
					"packet_dst_channel":       path.EndpointB.ChannelID,
					"packet_channel_ordering":  path.EndpointA.ChannelConfig.Order.String(),
					"packet_connection":        path.EndpointA.ConnectionID,
				},
				"message": {
					"module": "ibc_channel",
				},
			}

			if hasEvents {
				ibctesting.AssertEvents(suite.Suite, expEvents, events)
			} else {
				suite.Require().Len(events, 0)
			}
		})
	}
}

// TestRecvPacket test RecvPacket on chainB. Since packet commitment verification will always
// occur last (resource instensive), only tests expected to succeed and packet commitment
// verification tests need to simulate sending a packet from chainA to chainB.
func (suite *KeeperTestSuite) TestRecvPacket() {
	var (
		path       *ibctesting.Path
		packet     exported.PacketI
		channelCap *capabilitytypes.Capability
		expError   *sdkerrors.Error
		hasEvents  bool
	)

	testCases := []testCase{
		{"success: ORDERED channel", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			hasEvents = true
		}, true},
		{"success UNORDERED channel", func() {
			// setup uses an UNORDERED channel
			suite.coordinator.Setup(path)
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			hasEvents = true
		}, true},
		{"success with out of order packet: UNORDERED channel", func() {
			// setup uses an UNORDERED channel
			suite.coordinator.Setup(path)
			// send 2 packets
			_, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			// attempts to receive packet 2 without receiving packet 1
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			hasEvents = true
		}, true},
		{"packet already relayed ORDERED channel (no-op)", func() {
			expError = types.ErrNoOpMsg

			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)

			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			err = path.EndpointB.RecvPacket(packet.(types.Packet))
			suite.Require().NoError(err)
			hasEvents = true
		}, false},
		{"packet already relayed UNORDERED channel (no-op)", func() {
			expError = types.ErrNoOpMsg

			// setup uses an UNORDERED channel
			suite.coordinator.Setup(path)
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)

			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			err = path.EndpointB.RecvPacket(packet.(types.Packet))
			suite.Require().NoError(err)
			hasEvents = true
		}, false},
		{"out of order packet failure with ORDERED channel", func() {
			expError = types.ErrPacketSequenceOutOfOrder

			path.SetChannelOrdered()
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)

			// send 2 packets
			_, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			// attempts to receive packet 2 without receiving packet 1
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"channel not found", func() {
			expError = types.ErrChannelNotFound

			// use wrong channel naming
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, ibctesting.InvalidID, ibctesting.InvalidID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"channel not open", func() {
			expError = types.ErrInvalidChannelState

			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)

			err := path.EndpointB.SetChannelClosed()
			suite.Require().NoError(err)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"capability cannot authenticate ORDERED", func() {
			expError = types.ErrInvalidChannelCapability

			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = capabilitytypes.NewCapability(3)
		}, false},
		{"packet source port ≠ channel counterparty port", func() {
			expError = types.ErrInvalidPacket
			suite.coordinator.Setup(path)

			// use wrong port for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, ibctesting.InvalidID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"packet source channel ID ≠ channel counterparty channel ID", func() {
			expError = types.ErrInvalidPacket
			suite.coordinator.Setup(path)

			// use wrong port for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, ibctesting.InvalidID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"connection not found", func() {
			expError = connectiontypes.ErrConnectionNotFound
			suite.coordinator.Setup(path)

			// pass channel check
			suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainB.GetContext(),
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID), []string{connIDB}, path.EndpointB.ChannelConfig.Version),
			)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			suite.chainB.CreateChannelCapability(suite.chainB.GetSimApp().ScopedIBCMockKeeper, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"connection not OPEN", func() {
			expError = connectiontypes.ErrInvalidConnectionState
			suite.coordinator.SetupClients(path)

			// connection on chainB is in INIT
			err := path.EndpointB.ConnOpenInit()
			suite.Require().NoError(err)

			// pass channel check
			suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainB.GetContext(),
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID), []string{path.EndpointB.ConnectionID}, path.EndpointB.ChannelConfig.Version),
			)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			suite.chainB.CreateChannelCapability(suite.chainB.GetSimApp().ScopedIBCMockKeeper, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"timeout height passed", func() {
			expError = types.ErrPacketTimeout
			suite.coordinator.Setup(path)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, clienttypes.GetSelfHeight(suite.chainB.GetContext()), disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"timeout timestamp passed", func() {
			expError = types.ErrPacketTimeout
			suite.coordinator.Setup(path)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, disabledTimeoutHeight, uint64(suite.chainB.GetContext().BlockTime().UnixNano()))
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"next receive sequence is not found", func() {
			expError = types.ErrSequenceReceiveNotFound
			suite.coordinator.SetupConnections(path)

			path.EndpointA.ChannelID = ibctesting.FirstChannelID
			path.EndpointB.ChannelID = ibctesting.FirstChannelID

			// manually creating channel prevents next recv sequence from being set
			suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainB.GetContext(),
				path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID), []string{path.EndpointB.ConnectionID}, path.EndpointB.ChannelConfig.Version),
			)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)

			// manually set packet commitment
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetPacketCommitment(suite.chainA.GetContext(), path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, packet.GetSequence(), types.CommitPacket(suite.chainA.App.AppCodec(), packet))
			suite.chainB.CreateChannelCapability(suite.chainB.GetSimApp().ScopedIBCMockKeeper, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)

			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)

			path.EndpointA.UpdateClient()
			path.EndpointB.UpdateClient()
		}, false},
		{"receipt already stored", func() {
			expError = types.ErrNoOpMsg
			suite.coordinator.Setup(path)

			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)
			suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetPacketReceipt(suite.chainB.GetContext(), path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, sequence)
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			hasEvents = true
		}, false},
		{"validation failed", func() {
			// skip error code check, downstream error code is used from light-client implementations

			// packet commitment not set resulting in invalid proof
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
	}

	for i, tc := range testCases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s, %d/%d tests", tc.msg, i, len(testCases)), func() {
			suite.SetupTest() // reset
			expError = nil    // must explicitly set for failed cases
			hasEvents = false // reset events flag
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			tc.malleate()

			// get proof of packet commitment from chainA
			packetKey := host.PacketCommitmentKey(packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())
			proof, proofHeight := path.EndpointA.QueryProof(packetKey)

			ctx := suite.chainB.GetContext()
			err := suite.chainB.App.GetIBCKeeper().ChannelKeeper.RecvPacket(ctx, channelCap, packet, proof, proofHeight)

			if tc.expPass {
				suite.Require().NoError(err)

				channelB, _ := suite.chainB.App.GetIBCKeeper().ChannelKeeper.GetChannel(suite.chainB.GetContext(), packet.GetDestPort(), packet.GetDestChannel())
				nextSeqRecv, found := suite.chainB.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceRecv(suite.chainB.GetContext(), packet.GetDestPort(), packet.GetDestChannel())
				suite.Require().True(found)
				receipt, receiptStored := suite.chainB.App.GetIBCKeeper().ChannelKeeper.GetPacketReceipt(suite.chainB.GetContext(), packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence())

				if channelB.Ordering == types.ORDERED {
					suite.Require().Equal(packet.GetSequence()+1, nextSeqRecv, "sequence not incremented in ordered channel")
					suite.Require().False(receiptStored, "packet receipt stored on ORDERED channel")
				} else {
					suite.Require().Equal(uint64(1), nextSeqRecv, "sequence incremented for UNORDERED channel")
					suite.Require().True(receiptStored, "packet receipt not stored after RecvPacket in UNORDERED channel")
					suite.Require().Equal(string([]byte{byte(1)}), receipt, "packet receipt is not empty string")
				}
			} else {
				suite.Require().Error(err)

				// only check if expError is set, since not all error codes can be known
				if expError != nil {
					suite.Require().True(errors.Is(err, expError))
				}
			}

			// Verify events
			events := ctx.EventManager().Events()
			expEvents := map[string]map[string]string{
				"recv_packet": {
					"packet_data":              string(packet.GetData()),
					"packet_data_hex":          hex.EncodeToString(packet.GetData()),
					"packet_timeout_height":    packet.GetTimeoutHeight().String(),
					"packet_timeout_timestamp": fmt.Sprintf("%d", packet.GetTimeoutTimestamp()),
					"packet_sequence":          fmt.Sprintf("%d", packet.GetSequence()),
					"packet_src_port":          path.EndpointB.ChannelConfig.PortID,
					"packet_src_channel":       path.EndpointB.ChannelID,
					"packet_dst_port":          path.EndpointA.ChannelConfig.PortID,
					"packet_dst_channel":       path.EndpointA.ChannelID,
					"packet_channel_ordering":  path.EndpointB.ChannelConfig.Order.String(),
					"packet_connection":        path.EndpointB.ConnectionID,
				},
				"message": {
					"module": "ibc_channel",
				},
			}

			if hasEvents {
				ibctesting.AssertEvents(suite.Suite, expEvents, events)
			} else {
				suite.Require().Len(events, 0)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestWriteAcknowledgement() {
	var (
		path       *ibctesting.Path
		ack        exported.Acknowledgement
		packet     exported.PacketI
		channelCap *capabilitytypes.Capability
		hasEvents  bool
	)

	testCases := []testCase{
		{
			"success",
			func() {
				suite.coordinator.Setup(path)
				packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
				ack = ibcmock.MockAcknowledgement
				channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
				hasEvents = true
			},
			true,
		},
		{"channel not found", func() {
			// use wrong channel naming
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, ibctesting.InvalidID, ibctesting.InvalidID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			ack = ibcmock.MockAcknowledgement
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{"channel not open", func() {
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			ack = ibcmock.MockAcknowledgement

			err := path.EndpointB.SetChannelClosed()
			suite.Require().NoError(err)
			channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
		}, false},
		{
			"capability authentication failed",
			func() {
				suite.coordinator.Setup(path)
				packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
				ack = ibcmock.MockAcknowledgement
				channelCap = capabilitytypes.NewCapability(3)
			},
			false,
		},
		{
			"no-op, already acked",
			func() {
				suite.coordinator.Setup(path)
				packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
				ack = ibcmock.MockAcknowledgement
				suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetPacketAcknowledgement(suite.chainB.GetContext(), packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence(), ack.Acknowledgement())
				channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			},
			false,
		},
		{
			"empty acknowledgement",
			func() {
				suite.coordinator.Setup(path)
				packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
				ack = ibcmock.NewEmptyAcknowledgement()
				channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			},
			false,
		},
		{
			"acknowledgement is nil",
			func() {
				suite.coordinator.Setup(path)
				packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
				ack = nil
				channelCap = suite.chainB.GetChannelCapability(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)
			},
			false,
		},
	}
	for i, tc := range testCases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s, %d/%d tests", tc.msg, i, len(testCases)), func() {
			suite.SetupTest() // reset
			hasEvents = false // reset events flag
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			tc.malleate()

			ctx := suite.chainB.GetContext()
			err := suite.chainB.App.GetIBCKeeper().ChannelKeeper.WriteAcknowledgement(ctx, channelCap, packet, ack)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}

			// Verify events
			events := ctx.EventManager().Events()

			if hasEvents {
				expEvents := map[string]map[string]string{
					"write_acknowledgement": {
						"packet_data":              string(packet.GetData()),
						"packet_data_hex":          hex.EncodeToString(packet.GetData()),
						"packet_timeout_height":    packet.GetTimeoutHeight().String(),
						"packet_timeout_timestamp": fmt.Sprintf("%d", packet.GetTimeoutTimestamp()),
						"packet_sequence":          fmt.Sprintf("%d", packet.GetSequence()),
						"packet_src_port":          path.EndpointB.ChannelConfig.PortID,
						"packet_src_channel":       path.EndpointB.ChannelID,
						"packet_dst_port":          path.EndpointA.ChannelConfig.PortID,
						"packet_dst_channel":       path.EndpointA.ChannelID,
						"packet_ack":               string(ack.Acknowledgement()),
						"packet_ack_hex":           hex.EncodeToString(ack.Acknowledgement()),
						"packet_connection":        path.EndpointB.ConnectionID,
					},
					"message": {
						"module": "ibc_channel",
					},
				}

				ibctesting.AssertEvents(suite.Suite, expEvents, events)
			} else {
				suite.Require().Len(events, 0)
			}
		})
	}
}

// TestAcknowledgePacket tests the call AcknowledgePacket on chainA.
func (suite *KeeperTestSuite) TestAcknowledgePacket() {
	var (
		path   *ibctesting.Path
		packet types.Packet
		ack    = ibcmock.MockAcknowledgement

		channelCap *capabilitytypes.Capability
		expError   *sdkerrors.Error
		hasEvents  bool
	)

	testCases := []testCase{
		{"success on ordered channel", func() {
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			// create packet commitment
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)

			// create packet receipt and acknowledgement
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			hasEvents = true
		}, true},
		{"success on unordered channel", func() {
			// setup uses an UNORDERED channel
			suite.coordinator.Setup(path)

			// create packet commitment
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)

			// create packet receipt and acknowledgement
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			hasEvents = true
		}, true},
		{"packet already acknowledged ordered channel (no-op)", func() {
			expError = types.ErrNoOpMsg

			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			// create packet commitment
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)

			// create packet receipt and acknowledgement
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)

			err = path.EndpointA.AcknowledgePacket(packet, ack.Acknowledgement())
			suite.Require().NoError(err)
			hasEvents = true
		}, false},
		{"packet already acknowledged unordered channel (no-op)", func() {
			expError = types.ErrNoOpMsg

			// setup uses an UNORDERED channel
			suite.coordinator.Setup(path)

			// create packet commitment
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)

			// create packet receipt and acknowledgement
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)

			err = path.EndpointA.AcknowledgePacket(packet, ack.Acknowledgement())
			suite.Require().NoError(err)
			hasEvents = true
		}, false},
		{"channel not found", func() {
			expError = types.ErrChannelNotFound

			// use wrong channel naming
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, ibctesting.InvalidID, ibctesting.InvalidID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
		}, false},
		{"channel not open", func() {
			expError = types.ErrInvalidChannelState

			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)

			err := path.EndpointA.SetChannelClosed()
			suite.Require().NoError(err)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"capability authentication failed ORDERED", func() {
			expError = types.ErrInvalidChannelCapability

			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			// create packet commitment
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)

			// create packet receipt and acknowledgement
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			channelCap = capabilitytypes.NewCapability(3)
		}, false},
		{"packet destination port ≠ channel counterparty port", func() {
			expError = types.ErrInvalidPacket
			suite.coordinator.Setup(path)

			// use wrong port for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, ibctesting.InvalidID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet destination channel ID ≠ channel counterparty channel ID", func() {
			expError = types.ErrInvalidPacket
			suite.coordinator.Setup(path)

			// use wrong channel for dest
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, ibctesting.InvalidID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"connection not found", func() {
			expError = connectiontypes.ErrConnectionNotFound
			suite.coordinator.Setup(path)

			// pass channel check
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainA.GetContext(),
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID), []string{"connection-1000"}, path.EndpointA.ChannelConfig.Version),
			)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			suite.chainA.CreateChannelCapability(suite.chainA.GetSimApp().ScopedIBCMockKeeper, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"connection not OPEN", func() {
			expError = connectiontypes.ErrInvalidConnectionState
			suite.coordinator.SetupClients(path)
			// connection on chainA is in INIT
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// pass channel check
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainA.GetContext(),
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID), []string{path.EndpointA.ConnectionID}, path.EndpointA.ChannelConfig.Version),
			)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			suite.chainA.CreateChannelCapability(suite.chainA.GetSimApp().ScopedIBCMockKeeper, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet hasn't been sent", func() {
			expError = types.ErrNoOpMsg

			// packet commitment never written
			suite.coordinator.Setup(path)
			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			hasEvents = true
		}, false},
		{"packet ack verification failed", func() {
			// skip error code check since error occurs in light-clients

			// ack never written
			suite.coordinator.Setup(path)

			// create packet commitment
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
		{"packet commitment bytes do not match", func() {
			expError = types.ErrInvalidPacket

			// setup uses an UNORDERED channel
			suite.coordinator.Setup(path)

			// create packet commitment
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)

			// create packet receipt and acknowledgement
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)

			packet.Data = []byte("invalid packet commitment")
		}, false},
		{"next ack sequence not found", func() {
			expError = types.ErrSequenceAckNotFound
			suite.coordinator.SetupConnections(path)

			path.EndpointA.ChannelID = ibctesting.FirstChannelID
			path.EndpointB.ChannelID = ibctesting.FirstChannelID

			// manually creating channel prevents next sequence acknowledgement from being set
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetChannel(
				suite.chainA.GetContext(),
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				types.NewChannel(types.OPEN, types.ORDERED, types.NewCounterparty(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID), []string{path.EndpointA.ConnectionID}, path.EndpointA.ChannelConfig.Version),
			)

			packet = types.NewPacket(ibctesting.MockPacketData, 1, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			// manually set packet commitment
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetPacketCommitment(suite.chainA.GetContext(), path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, packet.GetSequence(), types.CommitPacket(suite.chainA.App.AppCodec(), packet))

			// manually set packet acknowledgement and capability
			suite.chainB.App.GetIBCKeeper().ChannelKeeper.SetPacketAcknowledgement(suite.chainB.GetContext(), path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, packet.GetSequence(), types.CommitAcknowledgement(ack.Acknowledgement()))

			suite.chainA.CreateChannelCapability(suite.chainA.GetSimApp().ScopedIBCMockKeeper, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)

			suite.coordinator.CommitBlock(path.EndpointA.Chain, path.EndpointB.Chain)

			path.EndpointA.UpdateClient()
			path.EndpointB.UpdateClient()
		}, false},
		{"next ack sequence mismatch ORDERED", func() {
			expError = types.ErrPacketSequenceOutOfOrder
			path.SetChannelOrdered()
			suite.coordinator.Setup(path)

			// create packet commitment
			sequence, err := path.EndpointA.SendPacket(defaultTimeoutHeight, disabledTimeoutTimestamp, ibctesting.MockPacketData)
			suite.Require().NoError(err)

			// create packet acknowledgement
			packet = types.NewPacket(ibctesting.MockPacketData, sequence, path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, defaultTimeoutHeight, disabledTimeoutTimestamp)
			err = path.EndpointB.RecvPacket(packet)
			suite.Require().NoError(err)

			// set next sequence ack wrong
			suite.chainA.App.GetIBCKeeper().ChannelKeeper.SetNextSequenceAck(suite.chainA.GetContext(), path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, 10)
			channelCap = suite.chainA.GetChannelCapability(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
		}, false},
	}

	for i, tc := range testCases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s, %d/%d tests", tc.msg, i, len(testCases)), func() {
			suite.SetupTest() // reset
			expError = nil    // must explcitly set error for failed cases
			hasEvents = false // reset events flag
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			tc.malleate()

			packetKey := host.PacketAcknowledgementKey(packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence())
			proof, proofHeight := path.EndpointB.QueryProof(packetKey)

			ctx := suite.chainA.GetContext()
			err := suite.chainA.App.GetIBCKeeper().ChannelKeeper.AcknowledgePacket(ctx, channelCap, packet, ack.Acknowledgement(), proof, proofHeight)
			pc := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetPacketCommitment(ctx, packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())

			channelA, _ := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetChannel(ctx, packet.GetSourcePort(), packet.GetSourceChannel())
			sequenceAck, _ := suite.chainA.App.GetIBCKeeper().ChannelKeeper.GetNextSequenceAck(ctx, packet.GetSourcePort(), packet.GetSourceChannel())

			if tc.expPass {
				suite.NoError(err)
				suite.Nil(pc)

				if channelA.Ordering == types.ORDERED {
					suite.Require().Equal(packet.GetSequence()+1, sequenceAck, "sequence not incremented in ordered channel")
				} else {
					suite.Require().Equal(uint64(1), sequenceAck, "sequence incremented for UNORDERED channel")
				}
			} else {
				suite.Error(err)
				// only check if expError is set, since not all error codes can be known
				if expError != nil {
					suite.Require().True(errors.Is(err, expError))
				}
			}

			// Verify events
			events := ctx.EventManager().Events()
			expEvents := map[string]map[string]string{
				"acknowledge_packet": {
					"packet_timeout_height":    packet.GetTimeoutHeight().String(),
					"packet_timeout_timestamp": fmt.Sprintf("%d", packet.GetTimeoutTimestamp()),
					"packet_sequence":          fmt.Sprintf("%d", packet.GetSequence()),
					"packet_src_port":          path.EndpointA.ChannelConfig.PortID,
					"packet_src_channel":       path.EndpointA.ChannelID,
					"packet_dst_port":          path.EndpointB.ChannelConfig.PortID,
					"packet_dst_channel":       path.EndpointB.ChannelID,
					"packet_channel_ordering":  path.EndpointA.ChannelConfig.Order.String(),
					"packet_connection":        path.EndpointA.ConnectionID,
				},
				"message": {
					"module": "ibc_channel",
				},
			}

			if hasEvents {
				ibctesting.AssertEvents(suite.Suite, expEvents, events)
			} else {
				suite.Require().Len(events, 0)
			}
		})
	}
}
