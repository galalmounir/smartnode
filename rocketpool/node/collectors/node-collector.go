package collectors

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rocket-pool/rocketpool-go/rocketpool"
	"github.com/rocket-pool/rocketpool-go/utils/eth"
	"github.com/rocket-pool/smartnode/shared/services/beacon"
	"github.com/rocket-pool/smartnode/shared/services/config"
	rprewards "github.com/rocket-pool/smartnode/shared/services/rewards"
	"github.com/rocket-pool/smartnode/shared/utils/eth2"
	"golang.org/x/sync/errgroup"
)

// Represents the collector for the user's node
type NodeCollector struct {
	// The total amount of RPL staked on the node
	totalStakedRpl *prometheus.Desc

	// The effective amount of RPL staked on the node (honoring the 150% collateral cap)
	effectiveStakedRpl *prometheus.Desc

	// The RPL collateral level for the node
	rplCollateral *prometheus.Desc

	// The cumulative RPL rewards earned by the node
	cumulativeRplRewards *prometheus.Desc

	// The expected RPL rewards for the node at the next rewards checkpoint
	expectedRplRewards *prometheus.Desc

	// The estimated APR of RPL for the node from the next rewards checkpoint
	rplApr *prometheus.Desc

	// The token balances of your node wallet
	balances *prometheus.Desc

	// The number of active minipools owned by the node
	activeMinipoolCount *prometheus.Desc

	// The amount of ETH this node deposited into minipools
	depositedEth *prometheus.Desc

	// The node's total share of its minipool's beacon chain balances
	beaconShare *prometheus.Desc

	// The total balances of all this node's validators on the beacon chain
	beaconBalance *prometheus.Desc

	// The RPL rewards from the last period that have not been claimed yet
	unclaimedRewards *prometheus.Desc

	// The claimed ETH rewards from the smoothing pool
	claimedEthRewards *prometheus.Desc

	// The unclaimed ETH rewards from the smoothing pool
	unclaimedEthRewards *prometheus.Desc

	// The total ETH rewards skimmed balance
	totalEthRewardsSkimmed *prometheus.Desc

	// The total ETH rewards share of the skimmed balance
	totalEthRewardsShareSkimmed *prometheus.Desc

	// The total refund ETH skimmed balance
	totalRefundEthSkimmed *prometheus.Desc

	// The Rocket Pool contract manager
	rp *rocketpool.RocketPool

	// The beacon client
	bc beacon.Client

	// The node's address
	nodeAddress common.Address

	// The event log interval for the current eth1 client
	eventLogInterval *big.Int

	// The next block to start from when looking at cumulative RPL rewards
	nextRewardsStartBlock *big.Int

	// The cumulative amount of RPL earned
	cumulativeRewards float64

	// The claimed ETH rewards from SP
	cumulativeClaimedEthRewards float64

	// Map of reward intervals that have already been processed
	handledIntervals map[uint64]bool

	// The Rocket Pool config
	cfg *config.RocketPoolConfig

	// The thread-safe locker for the network state
	stateLocker *StateLocker

	// Prefix for logging
	logPrefix string
}

// Create a new NodeCollector instance
func NewNodeCollector(rp *rocketpool.RocketPool, bc beacon.Client, nodeAddress common.Address, cfg *config.RocketPoolConfig, stateLocker *StateLocker) *NodeCollector {

	// Get the event log interval
	eventLogInterval, err := cfg.GetEventLogInterval()
	if err != nil {
		log.Printf("Error getting event log interval: %s\n", err.Error())
		return nil
	}

	subsystem := "node"
	return &NodeCollector{
		totalStakedRpl: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "total_staked_rpl"),
			"The total amount of RPL staked on the node",
			nil, nil,
		),
		effectiveStakedRpl: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "effective_staked_rpl"),
			"The effective amount of RPL staked on the node (honoring the 150% collateral cap)",
			nil, nil,
		),
		rplCollateral: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "rpl_collateral"),
			"The RPL collateral level for the node",
			nil, nil,
		),
		cumulativeRplRewards: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "cumulative_rpl_rewards"),
			"The cumulative RPL rewards earned by the node",
			nil, nil,
		),
		expectedRplRewards: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "expected_rpl_rewards"),
			"The expected RPL rewards for the node at the next rewards checkpoint",
			nil, nil,
		),
		rplApr: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "rpl_apr"),
			"The estimated APR of RPL for the node from the next rewards checkpoint",
			nil, nil,
		),
		balances: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "balance"),
			"How much ETH is in this node wallet",
			[]string{"Token"}, nil,
		),
		activeMinipoolCount: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "active_minipool_count"),
			"The number of active minipools owned by the node",
			nil, nil,
		),
		depositedEth: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "deposited_eth"),
			"The amount of ETH this node deposited into minipools",
			nil, nil,
		),
		beaconShare: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "beacon_share"),
			"The node's total share of its minipool's beacon chain balances",
			nil, nil,
		),
		beaconBalance: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "beacon_balance"),
			"The total balances of all this node's validators on the beacon chain",
			nil, nil,
		),
		unclaimedRewards: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "unclaimed_rewards"),
			"The RPL rewards from the last period that have not been claimed yet",
			nil, nil,
		),
		claimedEthRewards: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "claimed_eth_rewards"),
			"The claimed ETH rewards from the smoothing pool",
			nil, nil,
		),
		unclaimedEthRewards: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "unclaimed_eth_rewards"),
			"The unclaimed ETH rewards from the smoothing pool",
			nil, nil,
		),
		totalEthRewardsSkimmed: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "total_eth_rewards_skimmed"),
			"The total ETH rewards skimmed balance",
			nil, nil,
		),
		totalEthRewardsShareSkimmed: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "total_eth_rewards_share_skimmed"),
			"The total ETH rewards share of the skimmed balance",
			nil, nil,
		),
		totalRefundEthSkimmed: prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, "total_refund_eth_skimmed"),
			"The total refund ETH skimmed balance",
			nil, nil,
		),
		rp:               rp,
		bc:               bc,
		nodeAddress:      nodeAddress,
		eventLogInterval: big.NewInt(int64(eventLogInterval)),
		handledIntervals: map[uint64]bool{},
		cfg:              cfg,
		stateLocker:      stateLocker,
		logPrefix:        "Node Collector",
	}
}

// Write metric descriptions to the Prometheus channel
func (collector *NodeCollector) Describe(channel chan<- *prometheus.Desc) {
	channel <- collector.totalStakedRpl
	channel <- collector.effectiveStakedRpl
	channel <- collector.cumulativeRplRewards
	channel <- collector.expectedRplRewards
	channel <- collector.rplApr
	channel <- collector.balances
	channel <- collector.activeMinipoolCount
	channel <- collector.depositedEth
	channel <- collector.beaconShare
	channel <- collector.unclaimedRewards
	channel <- collector.claimedEthRewards
	channel <- collector.unclaimedEthRewards
	channel <- collector.totalEthRewardsShareSkimmed
	channel <- collector.totalEthRewardsSkimmed
	channel <- collector.totalRefundEthSkimmed
}

// Collect the latest metric values and pass them to Prometheus
func (collector *NodeCollector) Collect(channel chan<- prometheus.Metric) {
	// Get the latest state
	state := collector.stateLocker.GetState()
	if state == nil {
		return
	}

	nd := state.NodeDetailsByAddress[collector.nodeAddress]
	minipools := state.MinipoolDetailsByNode[collector.nodeAddress]

	// Sync
	var wg errgroup.Group
	stakedRpl := eth.WeiToEth(nd.RplStake)
	effectiveStakedRpl := eth.WeiToEth(nd.EffectiveRPLStake)
	rewardsInterval := state.NetworkDetails.IntervalDuration
	inflationInterval := state.NetworkDetails.RPLInflationIntervalRate
	totalRplSupply := state.NetworkDetails.RPLTotalSupply
	totalEffectiveStake := collector.stateLocker.GetTotalEffectiveRPLStake()
	nodeOperatorRewardsPercent := eth.WeiToEth(state.NetworkDetails.NodeOperatorRewardsPercent)
	ethBalance := eth.WeiToEth(nd.BalanceETH)
	oldRplBalance := eth.WeiToEth(nd.BalanceOldRPL)
	newRplBalance := eth.WeiToEth(nd.BalanceRPL)
	rethBalance := eth.WeiToEth(nd.BalanceRETH)
	var activeMinipoolCount float64
	rplPrice := eth.WeiToEth(state.NetworkDetails.RplPrice)
	collateralRatio := float64(0)
	var beaconHead beacon.BeaconHead
	unclaimedEthRewards := float64(0)
	unclaimedRplRewards := float64(0)
	if totalEffectiveStake == nil {
		return
	}

	// Get the cumulative claimed and unclaimed RPL rewards
	wg.Go(func() error {
		//legacyClaimNodeAddress := collector.cfg.Smartnode.GetLegacyClaimNodeAddress()
		//legacyRewardsPoolAddress := collector.cfg.Smartnode.GetLegacyRewardsPoolAddress()

		// Legacy rewards
		unclaimedRplWei := big.NewInt(0)
		unclaimedEthWei := big.NewInt(0)
		newRewards := big.NewInt(0)
		newClaimedEthRewards := big.NewInt(0)

		// TODO: PERFORMANCE IMPROVEMENTS
		/*newRewards, err := legacyrewards.CalculateLifetimeNodeRewards(collector.rp, collector.nodeAddress, collector.eventLogInterval, collector.nextRewardsStartBlock, &legacyRewardsPoolAddress, &legacyClaimNodeAddress)
		if err != nil {
			return fmt.Errorf("Error getting cumulative RPL rewards: %w", err)
		}*/

		// Get the claimed and unclaimed intervals
		unclaimed, claimed, err := rprewards.GetClaimStatus(collector.rp, collector.nodeAddress)
		if err != nil {
			return err
		}

		// Get the info for each claimed interval
		for _, claimedInterval := range claimed {
			_, exists := collector.handledIntervals[claimedInterval]
			if !exists {
				intervalInfo, err := rprewards.GetIntervalInfo(collector.rp, collector.cfg, collector.nodeAddress, claimedInterval)
				if err != nil {
					return err
				}
				if !intervalInfo.TreeFileExists {
					return fmt.Errorf("Error calculating lifetime node rewards: rewards file %s doesn't exist but interval %d was claimed", intervalInfo.TreeFilePath, claimedInterval)
				}

				newRewards.Add(newRewards, &intervalInfo.CollateralRplAmount.Int)
				newClaimedEthRewards.Add(newClaimedEthRewards, &intervalInfo.SmoothingPoolEthAmount.Int)
				collector.handledIntervals[claimedInterval] = true
			}
		}
		// Get the unclaimed rewards
		for _, unclaimedInterval := range unclaimed {
			intervalInfo, err := rprewards.GetIntervalInfo(collector.rp, collector.cfg, collector.nodeAddress, unclaimedInterval)
			if err != nil {
				return err
			}
			if !intervalInfo.TreeFileExists {
				return fmt.Errorf("Error calculating lifetime node rewards: rewards file %s doesn't exist and interval %d is unclaimed", intervalInfo.TreeFilePath, unclaimedInterval)
			}
			if intervalInfo.NodeExists {
				unclaimedRplWei.Add(unclaimedRplWei, &intervalInfo.CollateralRplAmount.Int)
				unclaimedEthWei.Add(unclaimedEthWei, &intervalInfo.SmoothingPoolEthAmount.Int)
			}
		}

		// Get the block for the next rewards checkpoint
		header, err := collector.rp.Client.HeaderByNumber(context.Background(), nil)
		if err != nil {
			return fmt.Errorf("Error getting latest block header: %w", err)
		}

		collector.cumulativeRewards += eth.WeiToEth(newRewards)
		collector.cumulativeClaimedEthRewards += eth.WeiToEth(newClaimedEthRewards)
		unclaimedRplRewards = eth.WeiToEth(unclaimedRplWei)
		unclaimedEthRewards = eth.WeiToEth(unclaimedEthWei)
		collector.nextRewardsStartBlock = big.NewInt(0).Add(header.Number, big.NewInt(1))

		return nil
	})

	// Get the number of active minipools on the node
	wg.Go(func() error {
		minipoolCount := len(minipools)
		for _, mpd := range minipools {
			if mpd.Finalised {
				minipoolCount--
			}
		}
		activeMinipoolCount = float64(minipoolCount)
		return nil
	})

	// Get the beacon head
	wg.Go(func() error {
		_beaconHead, err := collector.bc.GetBeaconHead()
		if err != nil {
			return fmt.Errorf("Error getting beacon chain head: %w", err)
		}
		beaconHead = _beaconHead
		return nil
	})

	// Wait for data
	if err := wg.Wait(); err != nil {
		collector.logError(err)
		return
	}

	// Calculate the estimated rewards
	rewardsIntervalDays := rewardsInterval.Seconds() / (60 * 60 * 24)
	inflationPerDay := eth.WeiToEth(inflationInterval)
	totalRplAtNextCheckpoint := (math.Pow(inflationPerDay, float64(rewardsIntervalDays)) - 1) * eth.WeiToEth(totalRplSupply)
	if totalRplAtNextCheckpoint < 0 {
		totalRplAtNextCheckpoint = 0
	}
	estimatedRewards := float64(0)
	if totalEffectiveStake.Cmp(big.NewInt(0)) == 1 {
		estimatedRewards = effectiveStakedRpl / eth.WeiToEth(totalEffectiveStake) * totalRplAtNextCheckpoint * nodeOperatorRewardsPercent
	}

	// Calculate the RPL APR
	rplApr := estimatedRewards / stakedRpl / rewardsInterval.Hours() * (24 * 365) * 100

	// Calculate the collateral ratio
	if activeMinipoolCount > 0 {
		collateralRatio = rplPrice * stakedRpl / (activeMinipoolCount * 16.0)
	}

	// Calculate the total deposits and corresponding beacon chain balance share
	opts := &bind.CallOpts{
		BlockNumber: big.NewInt(0).SetUint64(state.ElBlockNumber),
	}

	totalDistributableBalance := float64(0)
	totalNodeShareOfEthBalance := float64(0)
	totalRefundBalance := float64(0)
	for _, mpd := range minipools {
		totalNodeShareOfEthBalance += eth.WeiToEth(mpd.NodeShareOfBalance)
		totalRefundBalance += eth.WeiToEth(mpd.NodeRefundBalance)
		totalDistributableBalance += eth.WeiToEth(mpd.DistributableBalance)
	}

	minipoolDetails, err := eth2.GetBeaconBalancesFromState(collector.rp, minipools, state, beaconHead, opts)
	if err != nil {
		collector.logError(err)
		return
	}
	totalDepositBalance := float64(0)
	totalNodeShare := float64(0)
	totalBeaconBalance := float64(0)
	for _, minipool := range minipoolDetails {
		totalDepositBalance += eth.WeiToEth(minipool.NodeDeposit)
		totalNodeShare += eth.WeiToEth(minipool.NodeBalance)
		totalBeaconBalance += eth.WeiToEth(minipool.TotalBalance)
	}

	// Update all the metrics
	channel <- prometheus.MustNewConstMetric(
		collector.totalStakedRpl, prometheus.GaugeValue, stakedRpl)
	channel <- prometheus.MustNewConstMetric(
		collector.effectiveStakedRpl, prometheus.GaugeValue, effectiveStakedRpl)
	channel <- prometheus.MustNewConstMetric(
		collector.rplCollateral, prometheus.GaugeValue, collateralRatio)
	channel <- prometheus.MustNewConstMetric(
		collector.cumulativeRplRewards, prometheus.GaugeValue, collector.cumulativeRewards)
	channel <- prometheus.MustNewConstMetric(
		collector.expectedRplRewards, prometheus.GaugeValue, estimatedRewards)
	channel <- prometheus.MustNewConstMetric(
		collector.rplApr, prometheus.GaugeValue, rplApr)
	channel <- prometheus.MustNewConstMetric(
		collector.balances, prometheus.GaugeValue, ethBalance, "ETH")
	channel <- prometheus.MustNewConstMetric(
		collector.balances, prometheus.GaugeValue, oldRplBalance, "Legacy RPL")
	channel <- prometheus.MustNewConstMetric(
		collector.balances, prometheus.GaugeValue, newRplBalance, "New RPL")
	channel <- prometheus.MustNewConstMetric(
		collector.balances, prometheus.GaugeValue, rethBalance, "rETH")
	channel <- prometheus.MustNewConstMetric(
		collector.activeMinipoolCount, prometheus.GaugeValue, activeMinipoolCount)
	channel <- prometheus.MustNewConstMetric(
		collector.depositedEth, prometheus.GaugeValue, totalDepositBalance)
	channel <- prometheus.MustNewConstMetric(
		collector.beaconShare, prometheus.GaugeValue, totalNodeShare)
	channel <- prometheus.MustNewConstMetric(
		collector.beaconBalance, prometheus.GaugeValue, totalBeaconBalance)
	channel <- prometheus.MustNewConstMetric(
		collector.unclaimedRewards, prometheus.GaugeValue, unclaimedRplRewards)
	channel <- prometheus.MustNewConstMetric(
		collector.unclaimedEthRewards, prometheus.GaugeValue, unclaimedEthRewards)
	channel <- prometheus.MustNewConstMetric(
		collector.claimedEthRewards, prometheus.GaugeValue, collector.cumulativeClaimedEthRewards)
	channel <- prometheus.MustNewConstMetric(
		collector.totalEthRewardsShareSkimmed, prometheus.GaugeValue, totalNodeShareOfEthBalance)
	channel <- prometheus.MustNewConstMetric(
		collector.totalEthRewardsSkimmed, prometheus.GaugeValue, totalDistributableBalance)
	channel <- prometheus.MustNewConstMetric(
		collector.totalRefundEthSkimmed, prometheus.GaugeValue, totalRefundBalance)
}

// Log error messages
func (collector *NodeCollector) logError(err error) {
	fmt.Printf("[%s] %s\n", collector.logPrefix, err.Error())
}
