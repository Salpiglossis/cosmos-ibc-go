package types_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/iavl"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/stretchr/testify/suite"
	"github.com/cosmos/cosmos-sdk/db"
	"github.com/tendermint/tendermint/libs/log"

)

type MerkleTestSuite struct {
	suite.Suite

	store     *rootmulti.Store
	storeKey  *storetypes.KVStoreKey
	iavlStore *iavl.Store
}

func (suite *MerkleTestSuite) SetupTest() {
	dbm := db.NewDB()
	suite.store = rootmulti.NewStore(dbm, log.logger)

	suite.storeKey = storetypes.NewKVStoreKey("iavlStoreKey")

	suite.store.MountStoreWithDB(suite.storeKey, storetypes.StoreTypeIAVL, nil)
	suite.store.LoadVersion(0)

	suite.iavlStore = suite.store.GetCommitStore(suite.storeKey).(*iavl.Store)
}

func TestMerkleTestSuite(t *testing.T) {
	suite.Run(t, new(MerkleTestSuite))
}
