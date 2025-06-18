#!/usr/bin/env tsx
/**
 * dRPC Integration Test Multi-Environment Runner
 * 
 * Usage:
 *   tsx src/integration-test/integ.menv.ts                 # All environments (sequential)
 *   tsx src/integration-test/integ.menv.ts --env=node      # Node.js only
 *   tsx src/integration-test/integ.menv.ts --env=chrome    # Chrome only
 *   tsx src/integration-test/integ.menv.ts --env=firefox   # Firefox only
 */

import { createMenvWrapper } from '../../tests/util/menv-runner';

const wrapper = createMenvWrapper(
    'src/integration-test/drpc-client.integration.test.ts',
    'dRPC Integration Test Multi-Environment Runner'
);

wrapper.run().catch(error => {
    console.error('❌ Fatal error:', error);
    process.exit(1);
});
