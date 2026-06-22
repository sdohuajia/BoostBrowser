import { ComposedChart, Line, Area, Bar, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from 'recharts';

const data = [
  { name: 'Q1', 收入: 800, 支出: 300, 利润: 500, 增长率: 20 },
  { name: 'Q2', 收入: 967, 支出: 467, 利润: 500, 增长率: 10 },
  { name: 'Q3', 收入: 1098, 支出: 749, 利润: 349, 增长率: 15 },
  { name: 'Q4', 收入: 1200, 支出: 880, 利润: 320, 增长率: 12 },
  { name: 'Q5', 收入: 1108, 支出: 600, 利润: 508, 增长率: 22 },
  { name: 'Q6', 收入: 1300, 支出: 700, 利润: 600, 增长率: 25 },
];

export function ComposedChartExample() {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <ComposedChart
        data={data}
        margin={{ top: 20, right: 20, bottom: 20, left: 20 }}
      >
        <CartesianGrid stroke="var(--color-border-muted)" strokeDasharray="3 3" />
        <XAxis 
          dataKey="name" 
          tick={{ fill: 'var(--color-text-secondary)' }}
          label={{ value: '季度', position: 'insideBottomRight', offset: 0, fill: 'var(--color-text-secondary)' }}
        />
        <YAxis 
          yAxisId="left"
          tick={{ fill: 'var(--color-text-secondary)' }}
          label={{ value: '金额 (万元)', angle: -90, position: 'insideLeft', fill: 'var(--color-text-secondary)' }}
        />
        <YAxis
          yAxisId="right"
          orientation="right"
          tick={{ fill: 'var(--color-text-secondary)' }}
          label={{ value: '增长率 (%)', angle: 90, position: 'insideRight', fill: 'var(--color-text-secondary)' }}
        />
        <Tooltip
          contentStyle={{
            backgroundColor: 'var(--color-bg-default)',
            borderColor: 'var(--color-border-default)',
            color: 'var(--color-text-primary)'
          }}
        />
        <Legend wrapperStyle={{ color: 'var(--color-text-primary)' }} />
        <Area 
          yAxisId="left" 
          dataKey="利润" 
          fill="#8884d8" 
          stroke="#8884d8"
          fillOpacity={0.3} 
        />
        <Bar 
          yAxisId="left" 
          dataKey="收入" 
          barSize={20} 
          fill="var(--color-accent)"
          radius={[4, 4, 0, 0]}
        />
        <Bar 
          yAxisId="left" 
          dataKey="支出" 
          barSize={20} 
          fill="#82ca9d" 
          radius={[4, 4, 0, 0]}
        />
        <Line 
          yAxisId="right" 
          dataKey="增长率" 
          type="monotone" 
          stroke="#ff7300"
          strokeWidth={2}
          activeDot={{ r: 6 }} 
        />
      </ComposedChart>
    </ResponsiveContainer>
  );
}
