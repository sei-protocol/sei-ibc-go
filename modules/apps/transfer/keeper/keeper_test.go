package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/ibc-go/v7/modules/apps/transfer/types"
	ibctesting "github.com/cosmos/ibc-go/v7/testing"
)

type KeeperTestSuite struct {
	suite.Suite

	coordinator *ibctesting.Coordinator

	// testing chains used for convenience and readability
	chainA *ibctesting.TestChain
	chainB *ibctesting.TestChain
	chainC *ibctesting.TestChain
}

func (suite *KeeperTestSuite) SetupTest() {
	suite.coordinator = ibctesting.NewCoordinator(suite.T(), 3)
	suite.chainA = suite.coordinator.GetChain(ibctesting.GetChainID(1))
	suite.chainB = suite.coordinator.GetChain(ibctesting.GetChainID(2))
	suite.chainC = suite.coordinator.GetChain(ibctesting.GetChainID(3))

	queryHelper := baseapp.NewQueryServerTestHelper(suite.chainA.GetContext(), suite.chainA.GetSimApp().InterfaceRegistry())
	types.RegisterQueryServer(queryHelper, suite.chainA.GetSimApp().TransferKeeper)
}

func NewTransferPath(chainA, chainB *ibctesting.TestChain) *ibctesting.Path {
	path := ibctesting.NewPath(chainA, chainB)
	path.EndpointA.ChannelConfig.PortID = ibctesting.TransferPort
	path.EndpointB.ChannelConfig.PortID = ibctesting.TransferPort
	path.EndpointA.ChannelConfig.Version = types.Version
	path.EndpointB.ChannelConfig.Version = types.Version

	return path
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) TestSetGetTotalEscrowForDenom() {
	const denom = "atom"
	var expAmount math.Int

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"success: with 0 escrow amount",
			func() {},
			true,
		},
		{
			"success: with escrow amount > 2^63",
			func() {
				expAmount, _ = math.NewIntFromString("100000000000000000000")
			},
			true,
		},
		{
			"failure: setter panics with negative escrow amount",
			func() {
				expAmount = math.NewInt(-1)
			},
			false,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset
			expAmount = math.ZeroInt()
			ctx := suite.chainA.GetContext()

			tc.malleate()

			if tc.expPass {
				suite.chainA.GetSimApp().TransferKeeper.SetTotalEscrowForDenom(ctx, sdk.NewCoin(denom, expAmount))
				total := suite.chainA.GetSimApp().TransferKeeper.GetTotalEscrowForDenom(ctx, denom)
				suite.Require().Equal(expAmount, total.Amount)
			} else {
				suite.Require().PanicsWithError("negative coin amount: -1", func() {
					suite.chainA.GetSimApp().TransferKeeper.SetTotalEscrowForDenom(ctx, sdk.NewCoin(denom, expAmount))
				})
				total := suite.chainA.GetSimApp().TransferKeeper.GetTotalEscrowForDenom(ctx, denom)
				suite.Require().Equal(math.ZeroInt(), total.Amount)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestGetAllDenomEscrows() {
	var (
		store           storetypes.KVStore
		cdc             codec.Codec
		expDenomEscrows sdk.Coins
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"success",
			func() {
				denom := "uatom"
				amount := math.NewInt(100)
				expDenomEscrows = append(expDenomEscrows, sdk.NewCoin(denom, amount))

				bz := cdc.MustMarshal(&sdk.IntProto{Int: amount})
				store.Set(types.TotalEscrowForDenomKey(denom), bz)
			},
			true,
		},
		{
			"success: multiple denoms",
			func() {
				denom := "uatom"
				amount := math.NewInt(100)
				expDenomEscrows = append(expDenomEscrows, sdk.NewCoin(denom, amount))

				bz := cdc.MustMarshal(&sdk.IntProto{Int: amount})
				store.Set(types.TotalEscrowForDenomKey(denom), bz)

				denom = "bar/foo"
				amount = math.NewInt(50)
				expDenomEscrows = append(expDenomEscrows, sdk.NewCoin(denom, amount))

				bz = cdc.MustMarshal(&sdk.IntProto{Int: amount})
				store.Set(types.TotalEscrowForDenomKey(denom), bz)
			},
			true,
		},
		{
			"success: denom with non-alphanumeric characters",
			func() {
				denom := "ibc/123-456"
				amount := math.NewInt(100)
				expDenomEscrows = append(expDenomEscrows, sdk.NewCoin(denom, amount))

				bz := cdc.MustMarshal(&sdk.IntProto{Int: amount})
				store.Set(types.TotalEscrowForDenomKey(denom), bz)
			},
			true,
		},
		{
			"failure: empty denom",
			func() {
				denom := ""
				amount := math.ZeroInt()

				bz := cdc.MustMarshal(&sdk.IntProto{Int: amount})
				store.Set(types.TotalEscrowForDenomKey(denom), bz)
			},
			false,
		},
		{
			"failure: wrong prefix key",
			func() {
				denom := "uatom"
				amount := math.ZeroInt()

				bz := cdc.MustMarshal(&sdk.IntProto{Int: amount})
				store.Set([]byte(fmt.Sprintf("wrong-prefix/%s", denom)), bz)
			},
			false,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			expDenomEscrows = sdk.Coins{}
			ctx := suite.chainA.GetContext()

			storeKey := suite.chainA.GetSimApp().GetKey(types.ModuleName)
			store = ctx.KVStore(storeKey)
			cdc = suite.chainA.App.AppCodec()

			tc.malleate()

			denomEscrows := suite.chainA.GetSimApp().TransferKeeper.GetAllTotalEscrowed(ctx)

			if tc.expPass {
				suite.Require().Len(expDenomEscrows, len(denomEscrows))
				suite.Require().ElementsMatch(expDenomEscrows, denomEscrows)
			} else {
				suite.Require().Empty(denomEscrows)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestParams() {
	testCases := []struct {
		name    string
		input   types.Params
		expPass bool
	}{
		// it is not possible to set invalid booleans
		{"success: set params false-false", types.NewParams(false, false), true},
		{"success: set params false-true", types.NewParams(false, true), true},
		{"success: set params true-false", types.NewParams(true, false), true},
		{"success: set params true-true", types.NewParams(true, true), true},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset
			ctx := suite.chainA.GetContext()
			if tc.expPass {
				suite.chainA.GetSimApp().TransferKeeper.SetParams(ctx, tc.input)
				expected := tc.input
				p := suite.chainA.GetSimApp().TransferKeeper.GetParams(ctx)
				suite.Require().Equal(expected, p)
			} else {
				suite.Require().Panics(func() {
					suite.chainA.GetSimApp().TransferKeeper.SetParams(ctx, tc.input)
				})
			}
		})
	}
}

func (suite *KeeperTestSuite) TestUnsetParams() {
	suite.SetupTest()

	ctx := suite.chainA.GetContext()
	store := suite.chainA.GetContext().KVStore(suite.chainA.GetSimApp().GetKey(types.ModuleName))
	store.Delete([]byte(types.ParamsKey))

	suite.Require().Panics(func() {
		suite.chainA.GetSimApp().TransferKeeper.GetParams(ctx)
	})
}
