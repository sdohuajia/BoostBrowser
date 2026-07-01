import { 
  BarChartExample, 
  LineChartExample, 
  PieChartExample, 
  AreaChartExample, 
  ComposedChartExample 
} from './components';

export function ChartsPage() {
  return (
    <div className="p-6 space-y-8">
      <div className="mb-6">
        <h1 className="text-2xl font-semibold text-[var(--color-text-primary)]">图表案例展示</h1>
        <p className="text-[var(--color-text-secondary)]">以下展示了多种美观的数据可视化图表案例</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* 柱状图 */}
        <div className="bg-[var(--color-bg-surface)] p-4 rounded-lg border border-[var(--color-border-default)] shadow-sm">
          <h2 className="text-lg font-medium mb-4 text-[var(--color-text-primary)]">柱状图示例</h2>
          <div className="h-[300px]">
            <BarChartExample />
          </div>
        </div>

        {/* 折线图 */}
        <div className="bg-[var(--color-bg-surface)] p-4 rounded-lg border border-[var(--color-border-default)] shadow-sm">
          <h2 className="text-lg font-medium mb-4 text-[var(--color-text-primary)]">折线图示例</h2>
          <div className="h-[300px]">
            <LineChartExample />
          </div>
        </div>

        {/* 饼图 */}
        <div className="bg-[var(--color-bg-surface)] p-4 rounded-lg border border-[var(--color-border-default)] shadow-sm">
          <h2 className="text-lg font-medium mb-4 text-[var(--color-text-primary)]">饼图示例</h2>
          <div className="h-[300px]">
            <PieChartExample />
          </div>
        </div>

        {/* 面积图 */}
        <div className="bg-[var(--color-bg-surface)] p-4 rounded-lg border border-[var(--color-border-default)] shadow-sm">
          <h2 className="text-lg font-medium mb-4 text-[var(--color-text-primary)]">面积图示例</h2>
          <div className="h-[300px]">
            <AreaChartExample />
          </div>
        </div>

        {/* 组合图表 */}
        <div className="bg-[var(--color-bg-surface)] p-4 rounded-lg border border-[var(--color-border-default)] shadow-sm col-span-1 md:col-span-2">
          <h2 className="text-lg font-medium mb-4 text-[var(--color-text-primary)]">复合图表示例</h2>
          <div className="h-[400px]">
            <ComposedChartExample />
          </div>
        </div>
      </div>
    </div>
  );
}
