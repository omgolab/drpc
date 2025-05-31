# Peer Discovery Optimization

This directory contains tools to optimize both the peer discovery interval and dial timeout for libp2p connections using an **Adaptive Grid Search** algorithm.

## Files

- `optimize-interval.test.ts` - Main optimization test using adaptive divide-and-conquer algorithm
- `ambient_relay.ts` - Test script that accepts interval parameter for manual testing

## Algorithm: Adaptive Grid Search with Progressive Refinement

The optimization uses a smart **divide-and-conquer** approach inspired by:
- **Coordinate Descent Optimization** 
- **Bayesian Optimization with Grid Search**
- **Successive Halving Algorithm**

### How it Works:
1. **Coarse Grid**: Start with a sparse grid over the entire parameter space (50-3000ms)
2. **Test & Evaluate**: Test candidates using success rate and speed metrics  
3. **Zoom In**: Focus the search around the best-performing region
4. **Refine**: Progressively narrow the search space (40% reduction per iteration)
5. **Converge**: Stop when search space is < 10% of original or max iterations reached

This approach **reduces testing time by ~95%** compared to brute force (testing ~150 combinations instead of 3,600).

## Quick Start

### Run Adaptive Optimization

To find the optimal interval and dial timeout combination using the smart algorithm:

```bash
cd /Volumes/Projects/business/AstronLab/test-console/drpc-rnd

# Run the full adaptive optimization (recommended)
tsx experiments/auto-relay/optimize-interval.test.ts

# Run with explicit 'full' mode 
tsx experiments/auto-relay/optimize-interval.test.ts full

# Run faster variant with smaller search space
tsx experiments/auto-relay/optimize-interval.test.ts custom
# or
tsx experiments/auto-relay/optimize-interval.test.ts fast
```

### Test with Specific Parameters

After optimization, test with the recommended interval and dial timeout:

```bash
cd /Volumes/Projects/business/AstronLab/test-console/drpc-rnd
time tsx experiments/auto-relay/ambient_relay.ts [INTERVAL_MS]
```

Examples:

```bash
# Test with default interval (500ms)
time tsx experiments/auto-relay/ambient_relay.ts

# Test with optimized interval (e.g., 300ms)
time tsx experiments/auto-relay/ambient_relay.ts 300

# Test with faster interval (e.g., 100ms)
time tsx experiments/auto-relay/ambient_relay.ts 100
```

## How It Works

### Optimization Process

1. **Dynamic Range Generation**: Tests intervals and dial timeouts from 50ms to 3000ms with 50ms increments
2. **Adaptive Grid Search**: Uses progressive refinement to zoom into optimal regions
3. **Smart Sampling**: Starts with 5×5 grid (25 candidates) per iteration, converges in ~6 iterations
4. **Multiple Attempts**: Each combination tested multiple times for statistical reliability
5. **Composite Scoring**: Uses `success_rate * 100 - geometric_mean` for optimization
6. **Progressive Convergence**: Search space reduces by 60% each iteration until < 10% of original
7. **Efficiency**: Tests ~150 combinations vs 3,600 brute force (95% reduction)

### Metrics Evaluated

- **Success Rate**: Percentage of successful connections (primary ranking factor)
- **Geometric Mean**: Average connection time (geometric mean is less sensitive to outliers)
- **Connection Methods**: Tracks which connection method was used
- **Failure Analysis**: Records failed attempts for debugging
- **Dual Parameter Optimization**: Simultaneously optimizes both interval and dial timeout

### Output Format

The optimization provides:

- Real-time progress with iteration-by-iteration adaptive refinement
- Best results from each iteration with convergence tracking  
- Final top 10 combinations ranked by composite score
- Recommended optimal interval and dial timeout combination
- Algorithm efficiency metrics showing reduction vs brute force
- Estimated vs actual testing time comparison

## Configuration

You can modify the optimization parameters in `optimize-interval.test.ts`:

```typescript
const DEFAULT_CONFIG: OptimizationConfig = {
    initialSpace: {
        intervalMin: 50,
        intervalMax: 3000,
        dialTimeoutMin: 50,
        dialTimeoutMax: 3000
    },
    stepSize: 50,
    samplesPerDimension: 5, // 5x5 = 25 candidates per iteration
    attemptsPerCandidate: 3,
    maxIterations: 6, // Should converge quickly with adaptive refinement
    timeoutMs: 25000,
    cooldownMs: 500,
    convergenceThreshold: 0.1 // Stop when search space is < 10% of original
};
```

### Performance Tuning

- **Faster Testing**: Reduce `samplesPerDimension` to 3 (9 candidates/iteration)
- **More Thorough**: Increase `attemptsPerCandidate` to 5 for better statistics
- **Different Range**: Adjust `initialSpace` to focus on specific parameter ranges
- **Convergence Speed**: Lower `convergenceThreshold` for more precise results

## Implementation Details

### Function Signature Change

The `discoverOptimalConnection` function now accepts an `intervalMs` parameter:

```typescript
export async function discoverOptimalConnection(
  h: Libp2p,
  targetPeerIdStr: string,
  timeoutMs: number = 30000,
  dialTimeoutMs: number = 2000,
  intervalMs: number = 500, // <- New parameter
): Promise<ConnectionResult>;
```

### Usage in Code

```typescript
// With custom interval and dial timeout
const result = await discoverOptimalConnection(h, targetPeerId, 25000, 1500, 300);

// With default parameters
const result = await discoverOptimalConnection(h, targetPeerId, 25000);
```

## Expected Results

Typical optimization results show:

- **Fast combinations (50-200ms intervals, 50-500ms dial timeouts)**: Higher CPU usage, fastest initial connections
- **Balanced combinations (300-750ms intervals, 500-1500ms dial timeouts)**: Optimal balance of performance and resource usage  
- **Conservative combinations (1000ms+ intervals, 1500ms+ dial timeouts)**: Lower resource usage, reliable but slower discovery

The adaptive algorithm automatically finds the optimal sweet spot by:

- **Iteration 1-2**: Broad exploration across entire parameter space
- **Iteration 3-4**: Focused refinement around promising regions  
- **Iteration 5-6**: Fine-tuning within converged optimal zone

Convergence typically occurs within 150-200 total tests vs 3,600 for brute force.
