import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from 'recharts';

const data = [
  { name: '周一', 访问量: 4000, 用户数: 2400 },
  { name: '周二', 访问量: 3000, 用户数: 1398 },
  { name: '周三', 访问量: 2000, 用户数: 9800 },
  { name: '周四', 访问量: 2780, 用户数: 3908 },
  { name: '周五', 访问量: 1890, 用户数: 4800 },
  { name: '周六', 访问量: 2390, 用户数: 3800 },
  { name: '周日', 访问量: 3490, 用户数: 4300 },
];

export function LineChartExample() {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <LineChart
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
        <Legend wrapperStyle={{ color: 'var(--color-text-primary)' }} />
        <Line 
          type="monotone" 
          dataKey="访问量" 
          stroke="var(--color-accent)" 
          strokeWidth={2}
          activeDot={{ r: 6 }}
        />
        <Line 
          type="monotone" 
          dataKey="用户数" 
          stroke="#82ca9d" 
          strokeWidth={2}
          activeDot={{ r: 6 }}
        />
      </LineChart>
    </ResponsiveContainer>
  );
}
