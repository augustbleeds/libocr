package ocr3types

import (
	"context"
	"time"

	"github.com/smartcontractkit/libocr/commontypes"
	"github.com/smartcontractkit/libocr/offchainreporting2plus/types"
)

type Outcome []byte

type ReportingPluginFactory[RI any] interface {
	// Creates a new reporting plugin instance. The instance may have
	// associated goroutines or hold system resources, which should be
	// released when its Close() function is called.
	NewReportingPlugin(ReportingPluginConfig) (ReportingPlugin[RI], ReportingPluginInfo, error)
}

type ReportingPluginConfig struct {
	ConfigDigest types.ConfigDigest

	// OracleID (index) of the oracle executing this ReportingPlugin instance.
	OracleID commontypes.OracleID

	// N is the total number of nodes.
	N int

	// F is an upper bound on the number of faulty nodes, i.e. there are assumed
	// to be at most F faulty nodes.
	F int

	// Encoded configuration for the contract
	OnchainConfig []byte

	// Encoded configuration for the ORR3Plugin disseminated through the
	// contract. This value is only passed through the contract, but otherwise
	// ignored by it.
	OffchainConfig []byte

	// Estimate of the duration between rounds. You should not rely on this
	// value being accurate. Rounds might occur more or less frequently than
	// estimated.
	//
	// This value is intended for estimating the load incurred by a
	// ReportingPlugin before running it and for configuring caches.
	EstimatedRoundInterval time.Duration

	// Maximum duration the ReportingPlugin's functions are allowed to take
	MaxDurationQuery                        time.Duration
	MaxDurationObservation                  time.Duration
	MaxDurationShouldAcceptAttestedReport   time.Duration
	MaxDurationShouldTransmitAcceptedReport time.Duration
}

type ReportWithInfo[RI any] struct {
	Report types.Report
	// Metadata about the report passed to transmitter, keyring, etc..., e.g.
	// to trace flow of report through the system.
	Info RI
}

type OutcomeContext struct {
	// SeqNr of an OCR3 round/outcome. This is guaranteed to increase
	// in increments of one, i.e. for each SeqNr exactly one Outcome will
	// be generated.
	// The initial SeqNr value is 1. Its PreviousOutcome is nil.
	SeqNr uint64
	// This is guaranteed (!) to be the unique outcome with sequence number
	// (SeqNr-1).
	PreviousOutcome Outcome

	// Deprecated: exposed for legacy compatibility, do not rely on this
	// unless you have a really good reason.
	Epoch uint64
	// Deprecated: exposed for legacy compatibility, do not rely on this
	// unless you have a really good reason.
	Round uint64
}

type Quorum int

const (
	// Guarantees at least one honest observation
	QuorumFPlusOne Quorum = types.MaxOracles + 1 + iota
	// Guarantees an honest majority of observations
	QuorumTwoFPlusOne
	// Guarantees that all sets of observations overlap in at least one honest oracle
	QuorumByzQuorum
	// Maximal number of observations we can rely on being available
	QuorumNMinusF
)

// A ReportingPlugin allows plugging custom logic into the OCR3 protocol. The
// OCR protocol handles cryptography, networking, ensuring that a sufficient
// number of nodes is in agreement about any report, transmitting the report to
// the contract, etc... The ReportingPlugin handles application-specific logic.
// To do so, the ReportingPlugin defines a number of callbacks that are called
// by the OCR protocol logic at certain points in the protocol's execution flow.
// The report generated by the ReportingPlugin must be in a format understood by
// contract that the reports are transmitted to.
//
// We assume that each correct node participating in the protocol instance will
// be running the same ReportingPlugin implementation. However, not all nodes
// may be correct; up to f nodes be faulty in arbitrary ways (aka byzantine
// faults). For example, faulty nodes could be down, have intermittent
// connectivity issues, send garbage messages, or be controlled by an adversary.
//
// For a protocol round where everything is working correctly, followers will
// call Observation, ValidateObservation, Outcome, and Reports. For each report,
// ShouldAcceptAttestedReport will be called as well. If
// ShouldAcceptAttestedReport returns true, ShouldTransmitAcceptedReport will be
// called. However, an ReportingPlugin must also correctly handle the case where
// faults occur.
//
// In particular, an ReportingPlugin must deal with cases where:
//
// - only a subset of the functions on the ReportingPlugin are invoked for a
// given round
//
// - an arbitrary number of seqnrs has been skipped between invocations of the
// ReportingPlugin
//
// - the observation returned by Observation is not included in the list of
// AttributedObservations passed to Report
//
// - a query or observation is malformed. (For defense in depth, it is also
// recommended that malformed outcomes are handled gracefully.)
//
// - instances of the ReportingPlugin run by different oracles have different
// call traces. E.g., the ReportingPlugin's Observation function may have been
// invoked on node A, but not on node B.
//
// All functions on an ReportingPlugin should be thread-safe.
//
// All functions that take a context as their first argument may still do cheap
// computations after the context expires, but should stop any blocking
// interactions with outside services (APIs, database, ...) and return as
// quickly as possible. (Rough rule of thumb: any such computation should not
// take longer than a few ms.) A blocking function may block execution of the
// entire protocol instance on its node!
//
// For a given OCR protocol instance, there can be many (consecutive) instances
// of an ReportingPlugin, e.g. due to software restarts. If you need
// ReportingPlugin state to survive across restarts, you should store it in the
// Outcome or persist it. An ReportingPlugin instance will only ever serve a
// single protocol instance.
type ReportingPlugin[RI any] interface {
	// Query creates a Query that is sent from the leader to all follower nodes
	// as part of the request for an observation. Be careful! A malicious leader
	// could equivocate (i.e. send different queries to different followers.)
	// Many applications will likely be better off always using an empty query
	// if the oracles don't need to coordinate on what to observe (e.g. in case
	// of a price feed) or the underlying data source offers an (eventually)
	// consistent view to different oracles (e.g. in case of observing a
	// blockchain).
	//
	// You may assume that the outctx.SeqNr is increasing monotonically (though
	// *not* strictly) across the lifetime of a protocol instance and that
	// outctx.previousOutcome contains the consensus outcome with sequence
	// number (outctx.SeqNr-1).
	Query(ctx context.Context, outctx OutcomeContext) (types.Query, error)

	// Observation gets an observation from the underlying data source. Returns
	// a value or an error.
	//
	// You may assume that the outctx.SeqNr is increasing monotonically (though
	// *not* strictly) across the lifetime of a protocol instance and that
	// outctx.previousOutcome contains the consensus outcome with sequence
	// number (outctx.SeqNr-1).
	Observation(ctx context.Context, outctx OutcomeContext, query types.Query) (types.Observation, error)

	// Should return an error if an observation isn't well-formed.
	// Non-well-formed  observations will be discarded by the protocol. This
	// function should be pure. This is called for each observation, don't do
	// anything slow in here.
	//
	// You may assume that the outctx.SeqNr is increasing monotonically (though
	// *not* strictly) across the lifetime of a protocol instance and that
	// outctx.previousOutcome contains the consensus outcome with sequence
	// number (outctx.SeqNr-1).
	ValidateObservation(outctx OutcomeContext, query types.Query, ao types.AttributedObservation) error

	// ObservationQuorum returns the minimum number of valid (according to
	// ValidateObservation) observations needed to construct an outcome.
	//
	// This function should be pure. Don't do anything slow in here.
	//
	// This is an advanced feature. The "default" approach (what OCR1 & OCR2
	// did) is to have an empty ValidateObservation function and return
	// QuorumTwoFPlusOne from this function.
	ObservationQuorum(outctx OutcomeContext, query types.Query) (Quorum, error)

	// Generates an outcome for a seqNr, typically based on the previous
	// outcome, the current query, and the current set of attributed
	// observations.
	//
	// This function should be pure. Don't do anything slow in here.
	//
	// You may assume that the outctx.SeqNr is increasing monotonically (though
	// *not* strictly) across the lifetime of a protocol instance and that
	// outctx.previousOutcome contains the consensus outcome with sequence
	// number (outctx.SeqNr-1).
	//
	// You may assume that all provided observations have been validated by
	// ValidateObservation.
	Outcome(outctx OutcomeContext, query types.Query, aos []types.AttributedObservation) (Outcome, error)

	// Generates a (possibly empty) list of reports from an outcome. Each report
	// will be signed and possibly be transmitted to the contract. (Depending on
	// ShouldAcceptAttestedReport & ShouldTransmitAcceptedReport)
	//
	// This function should be pure. Don't do anything slow in here.
	//
	// This is likely to change in the future. It will likely be returning a
	// list of report batches, where each batch goes into its own Merkle tree.
	//
	// You may assume that the outctx.SeqNr is increasing monotonically (though
	// *not* strictly) across the lifetime of a protocol instance and that
	// outctx.previousOutcome contains the consensus outcome with sequence
	// number (outctx.SeqNr-1).
	Reports(seqNr uint64, outcome Outcome) ([]ReportWithInfo[RI], error)

	// Decides whether a report should be accepted for transmission. Any report
	// passed to this function will have been attested, i.e. signed by f+1
	// oracles.
	//
	// Don't make assumptions about the seqNr order in which this function
	// is called.
	ShouldAcceptAttestedReport(context.Context, uint64, ReportWithInfo[RI]) (bool, error)

	// Decides whether the given report should actually be broadcast to the
	// contract. This is invoked just before the broadcast occurs. Any report
	// passed to this function will have been signed by a quorum of oracles and
	// been accepted by ShouldAcceptAttestedReport.
	//
	// Don't make assumptions about the seqNr order in which this function
	// is called.
	//
	// As mentioned above, you should gracefully handle only a subset of a
	// ReportingPlugin's functions being invoked for a given report. For
	// example, due to reloading persisted pending transmissions from the
	// database upon oracle restart, this function  may be called with reports
	// that no other function of this instance of this interface has ever
	// been invoked on.
	ShouldTransmitAcceptedReport(context.Context, uint64, ReportWithInfo[RI]) (bool, error)

	// If Close is called a second time, it may return an error but must not
	// panic. This will always be called when a plugin is no longer
	// needed, e.g. on shutdown of the protocol instance or shutdown of the
	// oracle node. This will only be called after any calls to other functions
	// of the plugin have completed.
	Close() error
}

// It's much easier to increase these than to decrease them, so we start with
// conservative values. Talk to the maintainers if you need higher limits for
// your plugin.
const (
	mib                     = 1024 * 1024
	MaxMaxQueryLength       = 5 * mib
	MaxMaxObservationLength = 1 * mib
	MaxMaxOutcomeLength     = 5 * mib
	MaxMaxReportLength      = 5 * mib
	MaxMaxReportCount       = 2000
)

type ReportingPluginLimits struct {
	// Maximum length in bytes of data returned by the plugin. Used for
	// defending against spam attacks.
	MaxQueryLength       int
	MaxObservationLength int
	MaxOutcomeLength     int
	MaxReportLength      int
	MaxReportCount       int
}

type ReportingPluginInfo struct {
	// Used for debugging purposes.
	Name string

	Limits ReportingPluginLimits
}
