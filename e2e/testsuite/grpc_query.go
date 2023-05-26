package testsuite

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govtypesv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govtypesv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"
	paramsproposaltypes "github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	intertxtypes "github.com/cosmos/interchain-accounts/x/inter-tx/types"
	"github.com/strangelove-ventures/interchaintest/v7/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v7/ibc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	controllertypes "github.com/cosmos/ibc-go/v7/modules/apps/27-interchain-accounts/controller/types"
	feetypes "github.com/cosmos/ibc-go/v7/modules/apps/29-fee/types"
	transfertypes "github.com/cosmos/ibc-go/v7/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v7/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v7/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v7/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v7/modules/core/exported"
)

// GRPCClients holds a reference to any GRPC clients that are needed by the tests.
// These should typically be used for query clients only. If we need to make changes, we should
// use E2ETestSuite.BroadcastMessages to broadcast transactions instead.
type GRPCClients struct {
	ClientQueryClient     clienttypes.QueryClient
	ConnectionQueryClient connectiontypes.QueryClient
	ChannelQueryClient    channeltypes.QueryClient
	TransferQueryClient   transfertypes.QueryClient
	FeeQueryClient        feetypes.QueryClient
	ICAQueryClient        controllertypes.QueryClient
	InterTxQueryClient    intertxtypes.QueryClient

	// SDK query clients
	GovQueryClient    govtypesv1beta1.QueryClient
	GovQueryClientV1  govtypesv1.QueryClient
	GroupsQueryClient grouptypes.QueryClient
	ParamsQueryClient paramsproposaltypes.QueryClient
	AuthQueryClient   authtypes.QueryClient
	AuthZQueryClient  authz.QueryClient

	ConsensusServiceClient tmservice.ServiceClient
}

// InitGRPCClients establishes GRPC clients with the given chain.
// The created GRPCClients can be retrieved with GetChainGRCPClients.
func (s *E2ETestSuite) InitGRPCClients(chain *cosmos.CosmosChain) {
	// Create a connection to the gRPC server.
	grpcConn, err := grpc.Dial(
		chain.GetHostGRPCAddress(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	s.Require().NoError(err)
	s.T().Cleanup(func() {
		if err := grpcConn.Close(); err != nil {
			s.T().Logf("failed closing GRPC connection to chain %s: %s", chain.Config().ChainID, err)
		}
	})

	if s.grpcClients == nil {
		s.grpcClients = make(map[string]GRPCClients)
	}

	s.grpcClients[chain.Config().ChainID] = GRPCClients{
		ClientQueryClient:      clienttypes.NewQueryClient(grpcConn),
		ChannelQueryClient:     channeltypes.NewQueryClient(grpcConn),
		TransferQueryClient:    transfertypes.NewQueryClient(grpcConn),
		FeeQueryClient:         feetypes.NewQueryClient(grpcConn),
		ICAQueryClient:         controllertypes.NewQueryClient(grpcConn),
		InterTxQueryClient:     intertxtypes.NewQueryClient(grpcConn),
		GovQueryClient:         govtypesv1beta1.NewQueryClient(grpcConn),
		GovQueryClientV1:       govtypesv1.NewQueryClient(grpcConn),
		GroupsQueryClient:      grouptypes.NewQueryClient(grpcConn),
		ParamsQueryClient:      paramsproposaltypes.NewQueryClient(grpcConn),
		AuthQueryClient:        authtypes.NewQueryClient(grpcConn),
		AuthZQueryClient:       authz.NewQueryClient(grpcConn),
		ConsensusServiceClient: tmservice.NewServiceClient(grpcConn),
	}
}

// Header defines an interface which is implemented by both the sdk block header and the cometbft Block Header.
// this interfaces allows us to use the same function to fetch the block header for both chains.
type Header interface {
	GetTime() time.Time
	GetLastCommitHash() []byte
}

// QueryClientState queries the client state on the given chain for the provided clientID.
func (s *E2ETestSuite) QueryClientState(ctx context.Context, chain ibc.Chain, clientID string) (ibcexported.ClientState, error) {
	queryClient := s.GetChainGRCPClients(chain).ClientQueryClient
	res, err := queryClient.ClientState(ctx, &clienttypes.QueryClientStateRequest{
		ClientId: clientID,
	})
	if err != nil {
		return nil, err
	}

	cfg := EncodingConfig()
	var clientState ibcexported.ClientState
	if err := cfg.InterfaceRegistry.UnpackAny(res.ClientState, &clientState); err != nil {
		return nil, err
	}

	return clientState, nil
}

// QueryClientStatus queries the status of the client by clientID
func (s *E2ETestSuite) QueryClientStatus(ctx context.Context, chain ibc.Chain, clientID string) (string, error) {
	queryClient := s.GetChainGRCPClients(chain).ClientQueryClient
	res, err := queryClient.ClientStatus(ctx, &clienttypes.QueryClientStatusRequest{
		ClientId: clientID,
	})
	if err != nil {
		return "", err
	}

	return res.Status, nil
}

// QueryConnection queries the connection end using the given chain and connection id.
func (s *E2ETestSuite) QueryConnection(ctx context.Context, chain ibc.Chain, connectionID string) (connectiontypes.ConnectionEnd, error) {
	queryClient := s.GetChainGRCPClients(chain).ConnectionQueryClient
	res, err := queryClient.Connection(ctx, &connectiontypes.QueryConnectionRequest{
		ConnectionId: connectionID,
	})
	if err != nil {
		return connectiontypes.ConnectionEnd{}, err
	}

	return *res.Connection, nil
}

// QueryChannel queries the channel on a given chain for the provided portID and channelID
func (s *E2ETestSuite) QueryChannel(ctx context.Context, chain ibc.Chain, portID, channelID string) (channeltypes.Channel, error) {
	queryClient := s.GetChainGRCPClients(chain).ChannelQueryClient
	res, err := queryClient.Channel(ctx, &channeltypes.QueryChannelRequest{
		PortId:    portID,
		ChannelId: channelID,
	})
	if err != nil {
		return channeltypes.Channel{}, err
	}

	return *res.Channel, nil
}

// QueryPacketCommitment queries the packet commitment on the given chain for the provided channel and sequence.
func (s *E2ETestSuite) QueryPacketCommitment(ctx context.Context, chain ibc.Chain, portID, channelID string, sequence uint64) ([]byte, error) {
	queryClient := s.GetChainGRCPClients(chain).ChannelQueryClient
	res, err := queryClient.PacketCommitment(ctx, &channeltypes.QueryPacketCommitmentRequest{
		PortId:    portID,
		ChannelId: channelID,
		Sequence:  sequence,
	})
	if err != nil {
		return nil, err
	}
	return res.Commitment, nil
}

// QueryTotalEscrowForDenom queries the total amount of tokens in escrow for a denom
func (s *E2ETestSuite) QueryTotalEscrowForDenom(ctx context.Context, chain ibc.Chain, denom string) (sdk.Coin, error) {
	queryClient := s.GetChainGRCPClients(chain).TransferQueryClient
	res, err := queryClient.TotalEscrowForDenom(ctx, &transfertypes.QueryTotalEscrowForDenomRequest{
		Denom: denom,
	})
	if err != nil {
		return sdk.Coin{}, err
	}

	return res.Amount, nil
}

// QueryInterchainAccount queries the interchain account for the given owner and connectionID.
func (s *E2ETestSuite) QueryInterchainAccount(ctx context.Context, chain ibc.Chain, owner, connectionID string) (string, error) {
	queryClient := s.GetChainGRCPClients(chain).ICAQueryClient
	res, err := queryClient.InterchainAccount(ctx, &controllertypes.QueryInterchainAccountRequest{
		Owner:        owner,
		ConnectionId: connectionID,
	})
	if err != nil {
		return "", err
	}
	return res.Address, nil
}

// QueryInterchainAccountLegacy queries the interchain account for the given owner and connectionID using the intertx module.
func (s *E2ETestSuite) QueryInterchainAccountLegacy(ctx context.Context, chain ibc.Chain, owner, connectionID string) (string, error) {
	queryClient := s.GetChainGRCPClients(chain).InterTxQueryClient
	res, err := queryClient.InterchainAccount(ctx, &intertxtypes.QueryInterchainAccountRequest{
		Owner:        owner,
		ConnectionId: connectionID,
	})
	if err != nil {
		return "", err
	}

	return res.InterchainAccountAddress, nil
}

// QueryIncentivizedPacketsForChannel queries the incentivized packets on the specified channel.
func (s *E2ETestSuite) QueryIncentivizedPacketsForChannel(
	ctx context.Context,
	chain *cosmos.CosmosChain,
	portId,
	channelId string,
) ([]*feetypes.IdentifiedPacketFees, error) {
	queryClient := s.GetChainGRCPClients(chain).FeeQueryClient
	res, err := queryClient.IncentivizedPacketsForChannel(ctx, &feetypes.QueryIncentivizedPacketsForChannelRequest{
		PortId:    portId,
		ChannelId: channelId,
	})
	if err != nil {
		return nil, err
	}
	return res.IncentivizedPackets, err
}

// QueryCounterPartyPayee queries the counterparty payee of the given chain and relayer address on the specified channel.
func (s *E2ETestSuite) QueryCounterPartyPayee(ctx context.Context, chain ibc.Chain, relayerAddress, channelID string) (string, error) {
	queryClient := s.GetChainGRCPClients(chain).FeeQueryClient
	res, err := queryClient.CounterpartyPayee(ctx, &feetypes.QueryCounterpartyPayeeRequest{
		ChannelId: channelID,
		Relayer:   relayerAddress,
	})
	if err != nil {
		return "", err
	}
	return res.CounterpartyPayee, nil
}

// QueryProposal queries the governance proposal on the given chain with the given proposal ID.
func (s *E2ETestSuite) QueryProposal(ctx context.Context, chain ibc.Chain, proposalID uint64) (govtypesv1beta1.Proposal, error) {
	queryClient := s.GetChainGRCPClients(chain).GovQueryClient
	res, err := queryClient.Proposal(ctx, &govtypesv1beta1.QueryProposalRequest{
		ProposalId: proposalID,
	})
	if err != nil {
		return govtypesv1beta1.Proposal{}, err
	}

	return res.Proposal, nil
}

func (s *E2ETestSuite) QueryProposalV1(ctx context.Context, chain ibc.Chain, proposalID uint64) (govtypesv1.Proposal, error) {
	queryClient := s.GetChainGRCPClients(chain).GovQueryClientV1
	res, err := queryClient.Proposal(ctx, &govtypesv1.QueryProposalRequest{
		ProposalId: proposalID,
	})
	if err != nil {
		return govtypesv1.Proposal{}, err
	}

	return *res.Proposal, nil
}

// GetBlockHeaderByHeight fetches the block header at a given height.
func (s *E2ETestSuite) GetBlockHeaderByHeight(ctx context.Context, chain ibc.Chain, height uint64) (Header, error) {
	tmService := s.GetChainGRCPClients(chain).ConsensusServiceClient
	res, err := tmService.GetBlockByHeight(ctx, &tmservice.GetBlockByHeightRequest{
		Height: int64(height),
	})
	if err != nil {
		return nil, err
	}

	// Clean up when v6 is not supported, see: https://github.com/cosmos/ibc-go/issues/3540
	// versions newer than 0.47 SDK use the SdkBlock field while versions older
	// than 0.47 SDK, which do not have the SdkBlock field, use the Block field.
	if res.SdkBlock != nil {
		return &res.SdkBlock.Header, nil
	}
	return &res.Block.Header, nil
}

// GetValidatorSetByHeight returns the validators of the given chain at the specified height. The returned validators
// are sorted by address.
func (s *E2ETestSuite) GetValidatorSetByHeight(ctx context.Context, chain ibc.Chain, height uint64) ([]*tmservice.Validator, error) {
	tmService := s.GetChainGRCPClients(chain).ConsensusServiceClient
	res, err := tmService.GetValidatorSetByHeight(ctx, &tmservice.GetValidatorSetByHeightRequest{
		Height: int64(height),
	})
	if err != nil {
		return nil, err
	}

	sort.SliceStable(res.Validators, func(i, j int) bool {
		return res.Validators[i].Address < res.Validators[j].Address
	})

	return res.Validators, nil
}

// QueryModuleAccountAddress returns the sdk.AccAddress of a given module name.
func (s *E2ETestSuite) QueryModuleAccountAddress(ctx context.Context, moduleName string, chain *cosmos.CosmosChain) (sdk.AccAddress, error) {
	authClient := s.GetChainGRCPClients(chain).AuthQueryClient

	resp, err := authClient.ModuleAccountByName(ctx, &authtypes.QueryModuleAccountByNameRequest{
		Name: moduleName,
	})
	if err != nil {
		return nil, err
	}

	cfg := EncodingConfig()

	var account authtypes.AccountI
	if err := cfg.InterfaceRegistry.UnpackAny(resp.Account, &account); err != nil {
		return nil, err
	}
	moduleAccount, ok := account.(authtypes.ModuleAccountI)
	if !ok {
		return nil, fmt.Errorf("failed to cast account: %T as ModuleAccount", moduleAccount)
	}

	return moduleAccount.GetAddress(), nil
}

// QueryGranterGrants returns all GrantAuthorizations for the given granterAddress.
func (s *E2ETestSuite) QueryGranterGrants(ctx context.Context, chain *cosmos.CosmosChain, granterAddress string) ([]*authz.GrantAuthorization, error) {
	authzClient := s.GetChainGRCPClients(chain).AuthZQueryClient
	queryRequest := &authz.QueryGranterGrantsRequest{
		Granter: granterAddress,
	}

	grants, err := authzClient.GranterGrants(ctx, queryRequest)
	if err != nil {
		return nil, err
	}

	return grants.Grants, nil
}
