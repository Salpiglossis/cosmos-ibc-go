package capability_test

import (
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/ibc-go/v7/testing/simapp"

	"github.com/cosmos/ibc-go/modules/capability"
	"github.com/cosmos/ibc-go/modules/capability/keeper"
	"github.com/cosmos/ibc-go/modules/capability/types"
)

const memStoreKey = "memory:mock"

type CapabilityTestSuite struct {
	suite.Suite
	cdc    codec.Codec
	ctx    sdk.Context
	app    *simapp.SimApp
	keeper *keeper.Keeper
	module module.AppModule
}

func (s *CapabilityTestSuite) SetupTest() {
	checkTx := false
	app := simapp.Setup()
	cdc := app.AppCodec()

	// create new keeper so we can define custom scoping before init and seal
	keeper := keeper.NewKeeper(cdc, app.GetKey(types.StoreKey), app.GetMemKey(types.MemStoreKey))

	s.app = app
	s.ctx = app.BaseApp.NewContext(checkTx, tmproto.Header{Height: 1})
	s.keeper = keeper
	s.cdc = cdc
	s.module = capability.NewAppModule(cdc, *keeper, false)
}

// The following test case mocks a specific bug discovered in https://github.com/cosmos/cosmos-sdk/issues/9800
// and ensures that the current code successfully fixes the issue.
func (s *CapabilityTestSuite) TestInitializeMemStore() {
	sk1 := s.keeper.ScopeToModule(banktypes.ModuleName)

	cap1, err := sk1.NewCapability(s.ctx, "transfer")
	s.Require().NoError(err)
	s.Require().NotNil(cap1)

	// mock statesync by creating new keeper that shares persistent state but loses in-memory map
	newKeeper := keeper.NewKeeper(s.cdc, s.app.GetKey(types.StoreKey), s.app.GetMemKey(memStoreKey))
	newSk1 := newKeeper.ScopeToModule(banktypes.ModuleName)

	// Mock App startup
	ctx := s.app.BaseApp.NewUncachedContext(false, tmproto.Header{})
	newKeeper.Seal()
	s.Require().False(newKeeper.IsInitialized(ctx), "memstore initialized flag set before BeginBlock")

	// Mock app beginblock and ensure that no gas has been consumed and memstore is initialized
	ctx = s.app.BaseApp.NewContext(false, tmproto.Header{}).WithBlockGasMeter(sdk.NewGasMeter(50))

	prevBlockGas := ctx.BlockGasMeter().GasConsumed()
	prevGas := ctx.BlockGasMeter().GasConsumed()

	restartedModule := capability.NewAppModule(s.cdc, *newKeeper, true)
	restartedModule.BeginBlock(ctx, abci.RequestBeginBlock{})
	gasUsed := ctx.GasMeter().GasConsumed()

	s.Require().True(newKeeper.IsInitialized(ctx), "memstore initialized flag not set")
	blockGasUsed := ctx.BlockGasMeter().GasConsumed()

	s.Require().Equal(prevBlockGas, blockGasUsed, "ensure beginblocker consumed no block gas during execution")
	s.Require().Equal(prevGas, gasUsed, "ensure beginblocker consumed no gas during execution")

	// Mock the first transaction getting capability and subsequently failing
	// by using a cached context and discarding all cached writes.
	cacheCtx, _ := ctx.CacheContext()
	capability, ok := newSk1.GetCapability(cacheCtx, "transfer")
	s.Require().NotNil(capability)
	s.Require().True(ok)

	// Ensure that the second transaction can still receive capability even if first tx fails.
	ctx = s.app.BaseApp.NewContext(false, tmproto.Header{})

	cap1, ok = newSk1.GetCapability(ctx, "transfer")
	s.Require().True(ok)

	// Ensure the capabilities don't get reinitialized on next BeginBlock
	// by testing to see if capability returns same pointer
	// also check that initialized flag is still set
	restartedModule.BeginBlock(ctx, abci.RequestBeginBlock{})
	recap, ok := newSk1.GetCapability(ctx, "transfer")
	s.Require().True(ok)
	s.Require().Equal(cap1, recap, "capabilities got reinitialized after second BeginBlock")
	s.Require().True(newKeeper.IsInitialized(ctx), "memstore initialized flag not set")
}

func TestCapabilityTestSuite(t *testing.T) {
	suite.Run(t, new(CapabilityTestSuite))
}
