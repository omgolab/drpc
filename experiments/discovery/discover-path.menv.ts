#!/usr/bin/env tsx
/**
 * Discovery Path Multi-Environment Test Runner
 * 
 * Usage:
 *   tsx experiments/discovery/discover-path.menv.ts                 # All environments (sequential)
 *   tsx experiments/discovery/discover-path.menv.ts --env=node      # Node.js only
 *   tsx experiments/discovery/discover-path.menv.ts --env=chrome    # Chrome only
 *   tsx experiments/discovery/discover-path.menv.ts --env=firefox   # Firefox only
 */

import { createMenvWrapper } from '../../src/tests/util/menv-runner';

const wrapper = createMenvWrapper(
    'experiments/discovery/discover-path.test.ts',
    'Discovery Path Multi-Environment Test Runner'
);

wrapper.run().catch(error => {
    console.error('❌ Fatal error:', error);
    process.exit(1);
});
