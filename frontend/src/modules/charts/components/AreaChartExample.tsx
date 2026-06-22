import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from 'recharts';

const data = [
  { name: '周一', 系统A: 4000, 系统B: 2400, 系统C: 1800 },
  { name: '周二', 系统A: 3000, 系统B: 1398, 系统C: 2300 },
  { name: '周三', 系统A: 2000, 系统B: 9800, 系统C: 2500 },
  { name: '周四', 系统A: 2780, 系统B: 3908, 系统C: 1908 },
  { name: '周五', 系统A: 1890, 系统B: 4800, 系统C: 2800 },
  { name: '周六', 系统A: 2390, 系统B: 3800, 系统C: 3200 },
  { name: '周日', 系统A: 3490, 系统B: 4300, 系统C: 2100 },
];

export function AreaChartExample() {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart
        data={data}
        margin={{ top: 10, right: 30, left: 0, bottom: 0 }}
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
        <Area 
          type="monotone" 
          dataKey="系统A" 
          stackId="1"
          stroke="#8884d8" 
          fill="#8884d8"
          fillOpacity={0.6}
        />
        <Area 
          type="monotone" 
          dataKey="系统B" 
          stackId="1"
          stroke="#82ca9d" 
          fill="#82ca9d"
          fillOpacity={0.6}
        />
        <Area 
          type="monotone" 
          dataKey="系统C" 
          stackId="1"
          stroke="#ffc658" 
          fill="#ffc658"
          fillOpacity={0.6}
        />
      </AreaChart>
    </ResponsiveContainer>
  );
}
