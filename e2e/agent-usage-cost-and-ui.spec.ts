import { test, expect, type Page } from '@playwright/test'
import { registerAndOnboard } from './helpers'

// Canned stats rows: two agents across two buckets. Totals are chosen to
// exercise the compact formatters (164.92M tokens in, 2.53M out, 1,676
// sessions, 4,535 calls) and every cost tier ($1.03M / $12.3k / sub-$1).
const ROWS = [
  {
    bucket_start: '2026-09-17T10:00:00Z',
    agent_id: 'a1',
    agent_name: 'Alpha',
    user_id: 'u1',
    user_name: 'Ann',
    user_email: 'ann@example.com',
    sessions_count: 1000,
    messages_count: 1400,
    tokens_input: 100_000_000,
    tokens_output: 2_000_000,
    tokens_direct: 0,
    tokens_subagent: 0,
    model_calls: 1676,
    total_duration_ms: 4_044_188, // 2413 ms avg over 1676 calls
    est_cost_usd: 1_030_624.0,
  },
  {
    bucket_start: '2026-09-18T10:00:00Z',
    agent_id: 'a1',
    agent_name: 'Alpha',
    user_id: 'u1',
    user_name: 'Ann',
    user_email: 'ann@example.com',
    sessions_count: 0,
    messages_count: 0,
    tokens_input: 64_920_000,
    tokens_output: 530_000,
    tokens_direct: 0,
    tokens_subagent: 0,
    model_calls: 0,
    total_duration_ms: 0,
    est_cost_usd: 0,
  },
  {
    bucket_start: '2026-09-18T10:00:00Z',
    agent_id: 'a2',
    agent_name: 'Beta',
    user_id: 'u1',
    user_name: 'Ann',
    user_email: 'ann@example.com',
    sessions_count: 676,
    messages_count: 1013,
    tokens_input: 0,
    tokens_output: 0,
    tokens_direct: 0,
    tokens_subagent: 0,
    model_calls: 2859,
    total_duration_ms: 12_965_565, // 4535 ms avg over 2859 calls
    est_cost_usd: 12_345.6789,
  },
]

async function openMockedStatsPage(page: Page) {
  await page.route(/\/api\/v1\/agents\/stats\?/, (route) => route.fulfill({ json: ROWS }))
  await page.goto('/agents/stats')
  await expect(page.getByText('$1.04M')).toBeVisible({ timeout: 15_000 })
}

test.describe('Agent Usage tab', () => {
  test('renders the chart with a real height and compact cost KPIs', async ({ page }) => {
    test.setTimeout(60_000)
    await registerAndOnboard(page, Date.now().toString())
    await openMockedStatsPage(page)

    // Bug 2: the chart previously collapsed to zero height and never called
    // setOption. It must now be laid out and owned by an ECharts instance.
    const container = page.getByTestId('chart-container')
    await expect(container).toHaveAttribute('_echarts_instance_', /.+/, { timeout: 15_000 })
    const box = await container.boundingBox()
    expect(box).not.toBeNull()
    expect(box!.height).toBeGreaterThan(150)
    expect(box!.width).toBeGreaterThan(200)
    const canvas = container.locator('canvas')
    await expect(canvas).toHaveCount(1)
    const canvasBox = await canvas.boundingBox()
    expect(canvasBox!.height).toBeGreaterThan(150)

    // Bug 3: compact cost tiers; $1.04M total, $1.03M / $12.3k in the table.
    await expect(page.getByText('$1.04M')).toBeVisible()
    await expect(page.getByText('$1.03M')).toBeVisible()
    await expect(page.getByText('$12.3k')).toBeVisible()
    await expect(page.getByText('164.92M').first()).toBeVisible()
    await expect(page.getByText('2.53M').first()).toBeVisible()

    // KPI text must stay inside its card.
    const kpiValue = page.getByText('$1.04M')
    const kpiCard = kpiValue.locator('xpath=..')
    const valueBox = await kpiValue.boundingBox()
    const cardBox = await kpiCard.boundingBox()
    expect(valueBox!.x + valueBox!.width).toBeLessThanOrEqual(cardBox!.x + cardBox!.width + 1)

    // Cost chart mode still renders.
    await page.getByRole('button', { name: 'Cost', exact: true }).click()
    await expect(container.locator('canvas')).toHaveCount(1)
  })

  test.describe('pt-BR browser locale', () => {
    test.use({ locale: 'pt-BR' })

    test('pins en-US integer formatting next to the compact token formatters', async ({ page }) => {
      test.setTimeout(60_000)
      await registerAndOnboard(page, Date.now().toString())
      await openMockedStatsPage(page)

      // Bug 4: bare toLocaleString() rendered 1.676/4.535 in pt-BR, visually
      // identical to decimals beside "164.92M".
      await expect(page.getByText('1,676')).toBeVisible()
      await expect(page.getByText('4,535').first()).toBeVisible()
      await expect(page.getByText('2,413').first()).toBeVisible()
      await expect(page.getByText('1.676')).toHaveCount(0)
      await expect(page.getByText('4.535')).toHaveCount(0)
      await expect(page.getByText('2.413')).toHaveCount(0)
    })
  })
})
