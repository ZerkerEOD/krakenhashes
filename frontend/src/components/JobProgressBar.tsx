import React from 'react';
import { Box, Tooltip, Typography } from '@mui/material';
import { JobTask } from '../types/jobs';

interface JobProgressBarProps {
  tasks: JobTask[];
  totalKeyspace: number;
  height?: number;
}

interface TaskSegment {
  task: JobTask;
  startPercent: number;
  widthPercent: number;
  color: string;
  cracksFound: number;
}

const JobProgressBar: React.FC<JobProgressBarProps> = ({ 
  tasks, 
  totalKeyspace, 
  height = 40 
}) => {
  // Calculate segments for visualization
  const segments: TaskSegment[] = tasks.map(task => {
    // Use effective keyspace if available, otherwise use regular keyspace
    const start = task.effective_keyspace_start ?? task.keyspace_start;
    const end = task.effective_keyspace_end ?? task.keyspace_end;
    const processed = task.effective_keyspace_processed ?? task.keyspace_processed;
    
    const startPercent = (start / totalKeyspace) * 100;
    const widthPercent = ((end - start) / totalKeyspace) * 100;
    
    // Determine color based on status
    let color = '#e0e0e0'; // Default gray for pending
    if (task.status === 'running') {
      color = '#ffc107'; // Yellow for running
    } else if (task.status === 'processing') {
      color = '#2196f3'; // Blue for processing (hashcat done, saving to DB)
    } else if (task.status === 'completed') {
      color = '#4caf50'; // Green for completed
    } else if (task.status === 'processing_error') {
      color = '#ff9800'; // Orange for processing error
    } else if (task.status === 'failed') {
      color = '#f44336'; // Red for failed
    }
    
    return {
      task,
      startPercent,
      widthPercent,
      color,
      cracksFound: task.crack_count || 0
    };
  });

  // Calculate overall progress
  const totalProcessed = tasks.reduce((sum, task) => {
    const processed = task.effective_keyspace_processed ?? task.keyspace_processed;
    return sum + processed;
  }, 0);
  // Cap at 100% to prevent display issues when effective_keyspace from benchmark was lower than actual
  const rawProgress = totalKeyspace > 0 ? (totalProcessed / totalKeyspace) * 100 : 0;
  const overallProgress = Math.min(rawProgress, 100);

  const formatKeyspace = (value: number): string => {
    if (value >= 1e12) return `${(value / 1e12).toFixed(2)}T`;
    if (value >= 1e9) return `${(value / 1e9).toFixed(2)}B`;
    if (value >= 1e6) return `${(value / 1e6).toFixed(2)}M`;
    if (value >= 1e3) return `${(value / 1e3).toFixed(2)}K`;
    return value.toString();
  };

  const formatSpeed = (speed?: number): string => {
    if (!speed) return 'N/A';
    if (speed >= 1e12) return `${(speed / 1e12).toFixed(2)} TH/s`;
    if (speed >= 1e9) return `${(speed / 1e9).toFixed(2)} GH/s`;
    if (speed >= 1e6) return `${(speed / 1e6).toFixed(2)} MH/s`;
    if (speed >= 1e3) return `${(speed / 1e3).toFixed(2)} KH/s`;
    return `${speed} H/s`;
  };

  return (
    <Box sx={{ width: '100%' }}>
      {/* Progress percentage */}
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
        Overall Progress: {overallProgress.toFixed(2)}%
      </Typography>
      
      {/* Progress bar container */}
      <Box 
        sx={{ 
          position: 'relative',
          width: '100%',
          height: height,
          backgroundColor: '#f5f5f5',
          borderRadius: 1,
          overflow: 'hidden',
          border: '1px solid #ddd'
        }}
      >
        {/* Render segments */}
        {segments.map((segment, index) => (
          <Tooltip
            key={segment.task.id}
            title={
              <Box>
                <Typography variant="body2">Task ID: {segment.task.id.slice(0, 8)}</Typography>
                <Typography variant="body2">Status: {segment.task.status}</Typography>
                <Typography variant="body2">
                  Keyspace: {formatKeyspace(segment.task.effective_keyspace_start ?? segment.task.keyspace_start)} - {formatKeyspace(segment.task.effective_keyspace_end ?? segment.task.keyspace_end)}
                </Typography>
                <Typography variant="body2">
                  Progress: {segment.task.progress_percent?.toFixed(2) || 0}%
                </Typography>
                {segment.task.benchmark_speed && (
                  <Typography variant="body2">
                    Speed: {formatSpeed(segment.task.benchmark_speed)}
                  </Typography>
                )}
                {segment.cracksFound > 0 && (
                  <Typography variant="body2">
                    Cracks Found: {segment.cracksFound}
                  </Typography>
                )}
                {segment.task.agent_id && (
                  <Typography variant="body2">
                    Agent ID: {segment.task.agent_id}
                  </Typography>
                )}
              </Box>
            }
            arrow
            placement="top"
          >
            <Box
              sx={{
                position: 'absolute',
                left: `${segment.startPercent}%`,
                width: `${segment.widthPercent}%`,
                height: '100%',
                backgroundColor: segment.color,
                transition: 'all 0.3s ease',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                cursor: 'pointer',
                '&:hover': {
                  opacity: 0.8
                }
              }}
            >
              {/* Show progress within running tasks */}
              {segment.task.status === 'running' && segment.task.progress_percent && (
                <Box
                  sx={{
                    position: 'absolute',
                    left: 0,
                    width: `${segment.task.progress_percent}%`,
                    height: '100%',
                    backgroundColor: 'rgba(255, 255, 255, 0.3)',
                    transition: 'width 0.3s ease'
                  }}
                />
              )}
              
              {/* Render crack indicators as red vertical lines */}
              {segment.cracksFound > 0 && (
                <>
                  {Array.from({ length: Math.min(segment.cracksFound, 10) }).map((_, crackIndex) => {
                    // Distribute cracks evenly across the segment
                    const crackPosition = ((crackIndex + 1) / (segment.cracksFound + 1)) * 100;
                    return (
                      <Box
                        key={`crack-${segment.task.id}-${crackIndex}`}
                        sx={{
                          position: 'absolute',
                          left: `${crackPosition}%`,
                          width: '2px',
                          height: '100%',
                          backgroundColor: '#d32f2f',
                          zIndex: 2
                        }}
                      />
                    );
                  })}
                </>
              )}
            </Box>
          </Tooltip>
        ))}
        
        {/* If no tasks or keyspace is 0, show empty state */}
        {(segments.length === 0 || totalKeyspace === 0) && (
          <Box
            sx={{
              position: 'absolute',
              width: '100%',
              height: '100%',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center'
            }}
          >
            <Typography variant="body2" color="text.secondary">
              No tasks assigned yet
            </Typography>
          </Box>
        )}
      </Box>
      
      {/* Legend */}
      <Box sx={{ display: 'flex', gap: 2, mt: 1, flexWrap: 'wrap' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: '#e0e0e0', borderRadius: 0.5 }} />
          <Typography variant="caption">Pending</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: '#ffc107', borderRadius: 0.5 }} />
          <Typography variant="caption">Running</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: '#2196f3', borderRadius: 0.5 }} />
          <Typography variant="caption">Processing</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: '#4caf50', borderRadius: 0.5 }} />
          <Typography variant="caption">Completed</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: '#ff9800', borderRadius: 0.5 }} />
          <Typography variant="caption">Processing Error</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: '#f44336', borderRadius: 0.5 }} />
          <Typography variant="caption">Failed</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 2, height: 16, backgroundColor: '#d32f2f' }} />
          <Typography variant="caption">Crack Found</Typography>
        </Box>
      </Box>
    </Box>
  );
};

export default JobProgressBar;