import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from 'recharts';

const data = [
  { name: '1月', 产品A: 4000, 产品B: 2400 },
  { name: '2月', 产品A: 3000, 产品B: 1398 },
  { name: '3月', 产品A: 2000, 产品B: 9800 },
  { name: '4月', 产品A: 2780, 产品B: 3908 },
  { name: '5月', 产品A: 1890, 产品B: 4800 },
  { name: '6月', 产品A: 2390, 产品B: 3800 },
  { name: '7月', 产品A: 3490, 产品B: 4300 },
];

export function BarChartExample() {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <BarChart
        data={data}
        margin={{ top: 5, right: 30, left: 20, bottom: 5 }}
      >
        <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border-muted)" />
        <XAxis 
          dataKey="name" 
          tick={{ fill: 'var(--color-text-secondary)' }}
        />
        <YAxis 
          tick={{ fill: 'var(--color-text-secondary)' }}
        />
        <Tooltip
          contentStyle={{
            backgroundColor: 'var(--color-bg-default)',
            borderColor: 'var(--color-border-default)',
            color: 'var(--color-text-primary)'
          }}
        />
        <Legend 
          wrapperStyle={{ color: 'var(--color-text-primary)' }}
        />
        <Bar dataKey="产品A" fill="var(--color-accent)" radius={[4, 4, 0, 0]} />
        <Bar dataKey="产品B" fill="#82ca9d" radius={[4, 4, 0, 0]} />
      </BarChart>
    </ResponsiveContainer>
  );
}
