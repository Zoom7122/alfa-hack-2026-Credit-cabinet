//go:build integration

package financial

// Test-only exports keep cross-repository integration tests in financial_test.
// postgres depends on financial, so importing it from internal tests creates a cycle.
var (
	IntegrationDB                  = financialDB
	IntegrationNewID               = newID
	IntegrationHash                = hash
	IntegrationEnqueueTestDocument = enqueueTestDocument
	IntegrationExpireLease         = expireLease
	IntegrationRequested           = requested
	IntegrationResult              = testResult
	IntegrationLog                 = workerTestLog
)

type IntegrationProcessorFunc = processorFunc
