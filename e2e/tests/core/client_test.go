package e2e

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto/tmhash"
	tmjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/privval"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmprotoversion "github.com/cometbft/cometbft/proto/tendermint/version"
	tmtypes "github.com/cometbft/cometbft/types"
	tmversion "github.com/cometbft/cometbft/version"
	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	"github.com/strangelove-ventures/interchaintest/v7/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v7/ibc"
	test "github.com/strangelove-ventures/interchaintest/v7/testutil"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"

	"github.com/cosmos/ibc-go/e2e/dockerutil"
	"github.com/cosmos/ibc-go/e2e/testsuite"
	"github.com/cosmos/ibc-go/e2e/testvalues"
	clienttypes "github.com/cosmos/ibc-go/v7/modules/core/02-client/types"
	ibcexported "github.com/cosmos/ibc-go/v7/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v7/modules/light-clients/07-tendermint"
	ibctesting "github.com/cosmos/ibc-go/v7/testing"
	ibcmock "github.com/cosmos/ibc-go/v7/testing/mock"
)

const (
	invalidHashValue = "invalid_hash"
)

func TestClientTestSuite(t *testing.T) {
	suite.Run(t, new(ClientTestSuite))
}

type ClientTestSuite struct {
	testsuite.E2ETestSuite
}

// Status queries the current status of the client
func (s *ClientTestSuite) Status(ctx context.Context, chain ibc.Chain, clientID string) (string, error) {
	queryClient := s.GetChainGRCPClients(chain).ClientQueryClient
	res, err := queryClient.ClientStatus(ctx, &clienttypes.QueryClientStatusRequest{
		ClientId: clientID,
	})
	if err != nil {
		return "", err
	}

	return res.Status, nil
}

func (s *ClientTestSuite) TestClientUpdateProposal_Succeeds() {
	t := s.T()
	ctx := context.TODO()

	var (
		pathName           string
		relayer            ibc.Relayer
		subjectClientID    string
		substituteClientID string
		// set the trusting period to a value which will still be valid upon client creation, but invalid before the first update
		badTrustingPeriod = time.Duration(time.Second * 10)
	)

	t.Run("create substitute client with correct trusting period", func(t *testing.T) {
		relayer, _ = s.SetupChainsRelayerAndChannel(ctx)

		// TODO: update when client identifier created is accessible
		// currently assumes first client is 07-tendermint-0
		substituteClientID = clienttypes.FormatClientIdentifier(ibcexported.Tendermint, 0)

		// TODO: replace with better handling of path names
		pathName = fmt.Sprintf("%s-path-%d", s.T().Name(), 0)
		pathName = strings.ReplaceAll(pathName, "/", "-")
	})

	chainA, chainB := s.GetChains()
	chainAWallet := s.CreateUserOnChainA(ctx, testvalues.StartingTokenAmount)

	t.Run("create subject client with bad trusting period", func(t *testing.T) {
		createClientOptions := ibc.CreateClientOptions{
			TrustingPeriod: badTrustingPeriod.String(),
		}

		s.SetupClients(ctx, relayer, createClientOptions)

		// TODO: update when client identifier created is accessible
		// currently assumes second client is 07-tendermint-1
		subjectClientID = clienttypes.FormatClientIdentifier(ibcexported.Tendermint, 1)
	})

	time.Sleep(badTrustingPeriod)

	t.Run("update substitute client", func(t *testing.T) {
		s.UpdateClients(ctx, relayer, pathName)
	})

	s.Require().NoError(test.WaitForBlocks(ctx, 1, chainA, chainB), "failed to wait for blocks")

	t.Run("check status of each client", func(t *testing.T) {
		t.Run("substitute should be active", func(t *testing.T) {
			status, err := s.Status(ctx, chainA, substituteClientID)
			s.Require().NoError(err)
			s.Require().Equal(ibcexported.Active.String(), status)
		})

		t.Run("subject should be expired", func(t *testing.T) {
			status, err := s.Status(ctx, chainA, subjectClientID)
			s.Require().NoError(err)
			s.Require().Equal(ibcexported.Expired.String(), status)
		})
	})

	t.Run("pass client update proposal", func(t *testing.T) {
		proposal := clienttypes.NewClientUpdateProposal(ibctesting.Title, ibctesting.Description, subjectClientID, substituteClientID)
		s.ExecuteGovProposal(ctx, chainA, chainAWallet, proposal)
	})

	t.Run("check status of each client", func(t *testing.T) {
		t.Run("substitute should be active", func(t *testing.T) {
			status, err := s.Status(ctx, chainA, substituteClientID)
			s.Require().NoError(err)
			s.Require().Equal(ibcexported.Active.String(), status)
		})

		t.Run("subject should be active", func(t *testing.T) {
			status, err := s.Status(ctx, chainA, subjectClientID)
			s.Require().NoError(err)
			s.Require().Equal(ibcexported.Active.String(), status)
		})
	})
}

func (s *ClientTestSuite) TestClient_Update_Misbehaviour() {
	t := s.T()
	ctx := context.TODO()

	var (
		trustedHeight   clienttypes.Height
		latestHeight    clienttypes.Height
		clientState     ibcexported.ClientState
		header          testsuite.Header
		signers         []tmtypes.PrivValidator
		validatorSet    []*tmtypes.Validator
		maliciousHeader *ibctm.Header
		err             error
	)

	relayer, _ := s.SetupChainsRelayerAndChannel(ctx)
	chainA, chainB := s.GetChains()

	s.Require().NoError(test.WaitForBlocks(ctx, 10, chainA, chainB))

	t.Run("update clients", func(t *testing.T) {
		err := relayer.UpdateClients(ctx, s.GetRelayerExecReporter(), s.GetPathName(0))
		s.Require().NoError(err)

		clientState, err = s.QueryClientState(ctx, chainA, ibctesting.FirstClientID)
		s.Require().NoError(err)
	})

	t.Run("fetch trusted height", func(t *testing.T) {
		tmClientState, ok := clientState.(*ibctm.ClientState)
		s.Require().True(ok)

		trustedHeight, ok = tmClientState.GetLatestHeight().(clienttypes.Height)
		s.Require().True(ok)
	})

	t.Run("update clients", func(t *testing.T) {
		err := relayer.UpdateClients(ctx, s.GetRelayerExecReporter(), s.GetPathName(0))
		s.Require().NoError(err)

		clientState, err = s.QueryClientState(ctx, chainA, ibctesting.FirstClientID)
		s.Require().NoError(err)
	})

	t.Run("fetch client state latest height", func(t *testing.T) {
		tmClientState, ok := clientState.(*ibctm.ClientState)
		s.Require().True(ok)

		latestHeight, ok = tmClientState.GetLatestHeight().(clienttypes.Height)
		s.Require().True(ok)
	})

	t.Run("create validator set", func(t *testing.T) {
		var validators []*tmservice.Validator

		t.Run("fetch block header at latest client state height", func(t *testing.T) {
			header, err = s.GetBlockHeaderByHeight(ctx, chainB, latestHeight.GetRevisionHeight())
			s.Require().NoError(err)
		})

		t.Run("get validators at latest height", func(t *testing.T) {
			validators, err = s.GetValidatorSetByHeight(ctx, chainB, latestHeight.GetRevisionHeight())
			s.Require().NoError(err)
		})

		t.Run("extract validator private keys", func(t *testing.T) {
			privateKeys := s.extractChainPrivateKeys(ctx, chainB)
			for i, pv := range privateKeys {
				pubKey, err := pv.GetPubKey()
				s.Require().NoError(err)

				validator := tmtypes.NewValidator(pubKey, validators[i].VotingPower)

				validatorSet = append(validatorSet, validator)
				signers = append(signers, pv)
			}
		})
	})

	t.Run("create malicious header", func(t *testing.T) {
		valSet := tmtypes.NewValidatorSet(validatorSet)
		maliciousHeader, err = createMaliciousTMHeader(chainB.Config().ChainID, int64(latestHeight.GetRevisionHeight()), trustedHeight,
			header.GetTime(), valSet, valSet, signers, header)
		s.Require().NoError(err)
	})

	t.Run("update client with duplicate misbehaviour header", func(t *testing.T) {
		rlyWallet := s.CreateUserOnChainA(ctx, testvalues.StartingTokenAmount)
		msgUpdateClient, err := clienttypes.NewMsgUpdateClient(ibctesting.FirstClientID, maliciousHeader, rlyWallet.FormattedAddress())
		s.Require().NoError(err)

		txResp := s.BroadcastMessages(ctx, chainA, rlyWallet, msgUpdateClient)
		s.AssertTxSuccess(txResp)
	})

	t.Run("ensure client status is frozen", func(t *testing.T) {
		status, err := s.QueryClientStatus(ctx, chainA, ibctesting.FirstClientID)
		s.Require().NoError(err)
		s.Require().Equal(ibcexported.Frozen.String(), status)
	})
}

// extractChainPrivateKeys returns a slice of tmtypes.PrivValidator which hold the private keys for all validator
// nodes for a given chain.
func (s *ClientTestSuite) extractChainPrivateKeys(ctx context.Context, chain *cosmos.CosmosChain) []tmtypes.PrivValidator {
	testContainers, err := dockerutil.GetTestContainers(s.T(), ctx, s.DockerClient)
	s.Require().NoError(err)

	var filePvs []privval.FilePVKey
	var pvs []tmtypes.PrivValidator
	for _, container := range testContainers {
		isNodeForDifferentChain := !strings.Contains(container.Names[0], chain.Config().ChainID)
		isFullNode := strings.Contains(container.Names[0], fmt.Sprintf("%s-fn", chain.Config().ChainID))
		if isNodeForDifferentChain || isFullNode {
			continue
		}

		validatorPrivKey := fmt.Sprintf("/var/cosmos-chain/%s/config/priv_validator_key.json", chain.Config().Name)
		privKeyFileContents, err := dockerutil.GetFileContentsFromContainer(ctx, s.DockerClient, container.ID, validatorPrivKey)
		s.Require().NoError(err)

		var filePV privval.FilePVKey
		err = tmjson.Unmarshal(privKeyFileContents, &filePV)
		s.Require().NoError(err)
		filePvs = append(filePvs, filePV)
	}

	// We sort by address as GetValidatorSetByHeight also sorts by address. When iterating over them, the index
	// will correspond to the correct ibcmock.PV.
	sort.SliceStable(filePvs, func(i, j int) bool {
		return filePvs[i].Address.String() < filePvs[j].Address.String()
	})

	for _, filePV := range filePvs {
		pvs = append(pvs, &ibcmock.PV{
			PrivKey: &ed25519.PrivKey{Key: filePV.PrivKey.Bytes()},
		})
	}

	return pvs
}

// createMaliciousTMHeader creates a header with the provided trusted height with an invalid app hash.
func createMaliciousTMHeader(chainID string, blockHeight int64, trustedHeight clienttypes.Height, timestamp time.Time, tmValSet, tmTrustedVals *tmtypes.ValidatorSet, signers []tmtypes.PrivValidator, oldHeader testsuite.Header) (*ibctm.Header, error) {
	tmHeader := tmtypes.Header{
		Version:            tmprotoversion.Consensus{Block: tmversion.BlockProtocol, App: 2},
		ChainID:            chainID,
		Height:             blockHeight,
		Time:               timestamp,
		LastBlockID:        ibctesting.MakeBlockID(make([]byte, tmhash.Size), 10_000, make([]byte, tmhash.Size)),
		LastCommitHash:     oldHeader.GetLastCommitHash(),
		ValidatorsHash:     tmValSet.Hash(),
		NextValidatorsHash: tmValSet.Hash(),
		DataHash:           tmhash.Sum([]byte(invalidHashValue)),
		ConsensusHash:      tmhash.Sum([]byte(invalidHashValue)),
		AppHash:            tmhash.Sum([]byte(invalidHashValue)),
		LastResultsHash:    tmhash.Sum([]byte(invalidHashValue)),
		EvidenceHash:       tmhash.Sum([]byte(invalidHashValue)),
		ProposerAddress:    tmValSet.Proposer.Address, //nolint:staticcheck
	}

	hhash := tmHeader.Hash()
	blockID := ibctesting.MakeBlockID(hhash, 3, tmhash.Sum([]byte(invalidHashValue)))
	voteSet := tmtypes.NewVoteSet(chainID, blockHeight, 1, tmproto.PrecommitType, tmValSet)

	commit, err := tmtypes.MakeCommit(blockID, blockHeight, 1, voteSet, signers, timestamp)
	if err != nil {
		return nil, err
	}

	signedHeader := &tmproto.SignedHeader{
		Header: tmHeader.ToProto(),
		Commit: commit.ToProto(),
	}

	valSet, err := tmValSet.ToProto()
	if err != nil {
		return nil, err
	}

	trustedVals, err := tmTrustedVals.ToProto()
	if err != nil {
		return nil, err
	}

	return &ibctm.Header{
		SignedHeader:      signedHeader,
		ValidatorSet:      valSet,
		TrustedHeight:     trustedHeight,
		TrustedValidators: trustedVals,
	}, nil
}
