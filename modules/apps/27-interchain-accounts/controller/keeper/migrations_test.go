package keeper_test

import (
	"fmt"
	"reflect"

	"cosmossdk.io/store/prefix"

	"github.com/cosmos/cosmos-sdk/codec"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"

	icacontrollerkeeper "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/controller/keeper"
	icacontrollertypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/controller/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
)

func (suite *KeeperTestSuite) TestAssertChannelCapabilityMigrations() {
	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"success",
			func() {},
			true,
		},
		{
			"channel with different port is filtered out",
			func() {
				portIDWithOutPrefix := ibctesting.MockPort
				suite.chainA.GetSimApp().IBCKeeper.ChannelKeeper.SetChannel(suite.chainA.GetContext(), portIDWithOutPrefix, ibctesting.FirstChannelID, channeltypes.Channel{
					ConnectionHops: []string{ibctesting.FirstConnectionID},
				})
			},
			true,
		},
		{
			"capability not found",
			func() {
				portIDWithPrefix := fmt.Sprintf("%s%s", icatypes.ControllerPortPrefix, "port-without-capability")
				suite.chainA.GetSimApp().IBCKeeper.ChannelKeeper.SetChannel(suite.chainA.GetContext(), portIDWithPrefix, ibctesting.FirstChannelID, channeltypes.Channel{
					ConnectionHops: []string{ibctesting.FirstConnectionID},
				})
			},
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()

			path := NewICAPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			err := SetupICAPath(path, ibctesting.TestAccAddress)
			suite.Require().NoError(err)

			tc.malleate()

			migrator := icacontrollerkeeper.NewMigrator(&suite.chainA.GetSimApp().ICAControllerKeeper)
			err = migrator.AssertChannelCapabilityMigrations(suite.chainA.GetContext())

			if tc.expPass {
				suite.Require().NoError(err)

				isMiddlewareEnabled := suite.chainA.GetSimApp().ICAControllerKeeper.IsMiddlewareEnabled(
					suite.chainA.GetContext(),
					path.EndpointA.ChannelConfig.PortID,
					path.EndpointA.ConnectionID,
				)

				suite.Require().True(isMiddlewareEnabled)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestMigratorMigrateParams() {
	testCases := []struct {
		msg            string
		malleate       func()
		expectedParams icacontrollertypes.Params
	}{
		{
			"success: default params",
			func() {
				params := icacontrollertypes.DefaultParams()
				sk := suite.chainA.GetSimApp().GetKey(paramtypes.StoreKey)
				store := suite.chainA.GetContext().KVStore(sk)
				controllerStore := prefix.NewStore(store, append([]byte(icacontrollertypes.SubModuleName), '/'))
				enabled := reflect.Indirect(reflect.ValueOf(params.ControllerEnabled)).Interface()
				aminoCodec := codec.NewLegacyAmino()
				enabledBz, err := aminoCodec.MarshalJSON(enabled)
				suite.Require().NoError(err)

				controllerStore.Set(icacontrollertypes.KeyControllerEnabled, enabledBz)
			},
			icacontrollertypes.DefaultParams(),
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("case %s", tc.msg), func() {
			suite.SetupTest() // reset

			tc.malleate() // explicitly set params

			migrator := icacontrollerkeeper.NewMigrator(&suite.chainA.GetSimApp().ICAControllerKeeper)
			err := migrator.MigrateParams(suite.chainA.GetContext())
			suite.Require().NoError(err)

			params := suite.chainA.GetSimApp().ICAControllerKeeper.GetParams(suite.chainA.GetContext())
			suite.Require().Equal(tc.expectedParams, params)
		})
	}
}
