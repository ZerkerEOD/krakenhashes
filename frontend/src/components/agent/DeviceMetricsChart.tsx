import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts';
import { Box, Typography } from '@mui/material';
import { ChartTooltipBox, useChartColors } from '../ui/charts';
import { format } from 'date-fns';

interface DeviceData {
  deviceId: number;
  deviceName: string;
  metrics: {
    [metricType: string]: Array<{
      timestamp: number;
      value: number;
    }>;
  };
}

interface DeviceMetricsChartProps {
  title: string;
  metricType: string;
  devices: DeviceData[];
  deviceStatuses?: Array<{  // Device enabled/disabled status
    device_id: number;
    enabled: boolean;
  }>;
  unit: string;
  yAxisDomain?: [number, number];
  showCumulative?: boolean; // For hash rate chart
  timeRange: string; // '20m', '1h', '5h', '24h'
}


const DeviceMetricsChart: React.FC<DeviceMetricsChartProps> = ({
  title,
  metricType,
  devices,
  deviceStatuses,
  unit,
  yAxisDomain,
  showCumulative = false,
  timeRange,
}) => {
  const { t } = useTranslation('agents');
  const colors = useChartColors();
  // Calculate time range in milliseconds
  const getTimeRangeMs = () => {
    switch (timeRange) {
      case '10m': return 10 * 60 * 1000;
      case '20m': return 20 * 60 * 1000;
      case '1h': return 60 * 60 * 1000;
      case '5h': return 5 * 60 * 60 * 1000;
      case '24h': return 24 * 60 * 60 * 1000;
      default: return 10 * 60 * 1000; // Default to 10 minutes
    }
  };

  // Calculate fixed time domain
  const now = Date.now();
  const timeRangeMs = getTimeRangeMs();
  const startTime = now - timeRangeMs;
  const endTime = now;

  // Calculate interval to get approximately 100 data points
  const calculateInterval = () => {
    const targetPoints = 100;
    const intervalMs = Math.max(5000, Math.floor(timeRangeMs / targetPoints));
    
    // Round to nice intervals
    if (intervalMs <= 10000) return 5000;      // 5 seconds
    if (intervalMs <= 30000) return 15000;     // 15 seconds
    if (intervalMs <= 60000) return 30000;     // 30 seconds
    if (intervalMs <= 180000) return 60000;    // 1 minute
    if (intervalMs <= 600000) return 300000;   // 5 minutes
    return 900000;                              // 15 minutes
  };

  // Generate complete timeline with dynamic intervals
  const generateTimeline = () => {
    const timeline = [];
    const interval = calculateInterval();
    
    for (let time = startTime; time <= endTime; time += interval) {
      timeline.push(time);
    }
    
    return timeline;
  };

  // Prepare data for the chart
  const prepareChartData = () => {
    // Always return full timeline, even with no devices
    const baseTimeline = generateTimeline().map(timestamp => ({
      timestamp,
      time: format(new Date(timestamp), 'HH:mm:ss'),
    }));

    if (!devices || devices.length === 0) {
      return baseTimeline;
    }

    // Generate the complete timeline
    const timeline = generateTimeline();

    // Create a map of all data points by device
    const dataByDevice = new Map<number, Array<{timestamp: number, value: number}>>();
    
    devices.forEach(device => {
      const metrics = device.metrics[metricType] || [];
      const deviceData: Array<{timestamp: number, value: number}> = [];
      
      metrics.forEach(metric => {
        // Only include metrics within our time range
        if (metric.timestamp >= startTime && metric.timestamp <= endTime) {
          deviceData.push({
            timestamp: metric.timestamp,
            value: metric.value
          });
        }
      });
      
      // Sort by timestamp
      deviceData.sort((a, b) => a.timestamp - b.timestamp);
      dataByDevice.set(device.deviceId, deviceData);
    });

    // Helper function to average data points within a time window
    const averageDataInWindow = (deviceData: Array<{timestamp: number, value: number}>, windowStart: number, windowEnd: number) => {
      if (!deviceData || deviceData.length === 0) return null;
      
      const pointsInWindow = deviceData.filter(
        d => d.timestamp >= windowStart && d.timestamp < windowEnd
      );
      
      if (pointsInWindow.length === 0) return null;
      
      const sum = pointsInWindow.reduce((acc, p) => acc + p.value, 0);
      return sum / pointsInWindow.length;
    };

    // Create map of device enabled status
    const deviceEnabledMap = new Map<number, boolean>();
    if (deviceStatuses) {
      deviceStatuses.forEach(device => {
        deviceEnabledMap.set(device.device_id, device.enabled);
      });
    }

    // Build chart data with complete timeline
    const chartData = timeline.map(timestamp => {
      const dataPoint: any = {
        timestamp,
        time: format(new Date(timestamp), 'HH:mm:ss'),
      };

      let cumulativeValue = 0;

      const interval = calculateInterval();
      const halfInterval = interval / 2;
      
      devices.forEach((device) => {
        const deviceData = dataByDevice.get(device.deviceId);
        const isEnabled = deviceEnabledMap.get(device.deviceId) ?? true; // Default to true if no status
        
        // Average data points within the interval window centered on this timestamp
        const value = deviceData ? 
          averageDataInWindow(deviceData, timestamp - halfInterval, timestamp + halfInterval) : 
          null;
        
        if (value !== null) {
          dataPoint[`device_${device.deviceId}`] = value;
          cumulativeValue += value;
        } else {
          // Smart handling based on device status and time range
          if (!isEnabled) {
            // Disabled device - always show gap
            dataPoint[`device_${device.deviceId}`] = null;
          } else {
            // Enabled device - decide based on time range and data presence
            if (timeRange === '10m' || timeRange === '20m' || timeRange === '1h') {
              // Short time ranges - always show 0
              dataPoint[`device_${device.deviceId}`] = 0;
            } else {
              // Long time ranges - check if device has ANY data in the range
              const hasDataInRange = deviceData && deviceData.some(
                d => d.timestamp >= startTime && d.timestamp <= endTime
              );
              dataPoint[`device_${device.deviceId}`] = hasDataInRange ? 0 : null;
            }
          }
        }
      });

      if (showCumulative) {
        dataPoint.cumulative = cumulativeValue > 0 ? cumulativeValue : null;
      }

      return dataPoint;
    });

    // Ensure we have boundary points at start and end of timeline
    if (chartData.length > 0) {
      // Check if we need to add a point at the start
      if (chartData[0].timestamp > startTime) {
        const startPoint: any = {
          timestamp: startTime,
          time: format(new Date(startTime), 'HH:mm:ss'),
        };
        
        // Add null values for all devices
        devices.forEach((device) => {
          startPoint[`device_${device.deviceId}`] = null;
        });
        
        if (showCumulative) {
          startPoint.cumulative = null;
        }
        
        chartData.unshift(startPoint);
      }
      
      // Check if we need to add a point at the end
      if (chartData[chartData.length - 1].timestamp < endTime) {
        const endPoint: any = {
          timestamp: endTime,
          time: format(new Date(endTime), 'HH:mm:ss'),
        };
        
        // Add null values for all devices
        devices.forEach((device) => {
          endPoint[`device_${device.deviceId}`] = null;
        });
        
        if (showCumulative) {
          endPoint.cumulative = null;
        }
        
        chartData.push(endPoint);
      }
    }

    return chartData;
  };

  const chartData = prepareChartData();

  // Generate ticks for X-axis to force the full time range
  const generateXAxisTicks = () => {
    const ticks = [];
    const interval = calculateInterval();
    const tickCount = Math.min(10, Math.floor(timeRangeMs / interval));
    const tickInterval = tickCount > 0 ? timeRangeMs / tickCount : timeRangeMs / 10;
    
    for (let i = 0; i <= tickCount; i++) {
      ticks.push(startTime + (tickInterval * i));
    }
    
    return ticks;
  };

  // Format Y-axis values
  const formatYAxis = (value: number) => {
    if (metricType === 'hashrate') {
      // Format hash rate in human-readable format
      if (value >= 1e12) return `${(value / 1e12).toFixed(1)}TH/s`;
      if (value >= 1e9) return `${(value / 1e9).toFixed(1)}GH/s`;
      if (value >= 1e6) return `${(value / 1e6).toFixed(1)}MH/s`;
      if (value >= 1e3) return `${(value / 1e3).toFixed(1)}KH/s`;
      return `${value}H/s`;
    }
    return `${value}${unit}`;
  };

  // Custom tooltip
  const CustomTooltip = ({ active, payload, label }: any) => {
    if (active && payload && payload.length) {
      return (
        <ChartTooltipBox>
          <Typography variant="body2">{label}</Typography>
          {payload.map((entry: any, index: number) => (
            <Typography
              key={index}
              variant="body2"
              style={{ color: entry.color }}
            >
              {entry.name}: {formatYAxis(entry.value)}
            </Typography>
          ))}
        </ChartTooltipBox>
      );
    }
    return null;
  };

  if (chartData.length === 0) {
    return (
      <Box sx={{ height: 300, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Typography color="text.secondary">{t('monitoring.noDataAvailable')}</Typography>
      </Box>
    );
  }

  return (
    <Box>
      <Typography variant="h6" gutterBottom>
        {title}
      </Typography>
      <ResponsiveContainer width="100%" height={300}>
        <LineChart
          data={chartData}
          margin={{ top: 5, right: 30, left: 20, bottom: 5 }}
        >
          <CartesianGrid strokeDasharray="3 3" stroke={colors.grid} />
          <XAxis 
            dataKey="timestamp"
            stroke={colors.axis}
            type="number"
            scale="time"
            domain={[startTime, endTime]}
            allowDataOverflow={true}
            ticks={generateXAxisTicks()}
            tickFormatter={(timestamp) => format(new Date(timestamp), 'HH:mm:ss')}
          />
          <YAxis 
            tickFormatter={formatYAxis}
            stroke={colors.axis}
            domain={yAxisDomain}
          />
          <Tooltip content={<CustomTooltip />} />
          <Legend />
          
          {/* Lines for each device */}
          {devices.map((device, index) => (
            <Line
              key={device.deviceId}
              type="monotone"
              dataKey={`device_${device.deviceId}`}
              name={device.deviceName || (t('monitoring.deviceFallback', { id: device.deviceId }) as string)}
              stroke={colors.series[index % colors.series.length]}
              strokeWidth={2}
              dot={false}
              connectNulls={false} // This creates gaps for missing data
              isAnimationActive={false}
              animationDuration={0}
            />
          ))}
          
          {/* Cumulative line for hash rate */}
          {showCumulative && (
            <Line
              type="monotone"
              dataKey="cumulative"
              name={t('monitoring.allDevices') as string}
              stroke={colors.axis}
              strokeWidth={3}
              strokeDasharray="5 5"
              dot={false}
              connectNulls={false}
              isAnimationActive={false}
              animationDuration={0}
            />
          )}
        </LineChart>
      </ResponsiveContainer>
    </Box>
  );
};

export default DeviceMetricsChart;