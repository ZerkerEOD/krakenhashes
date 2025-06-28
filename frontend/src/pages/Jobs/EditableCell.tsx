import React, { useState } from 'react';
import {
  Box,
  TextField,
  IconButton,
  Typography,
  Tooltip,
  CircularProgress,
} from '@mui/material';
import {
  Edit as EditIcon,
  Check as CheckIcon,
  Close as CloseIcon,
} from '@mui/icons-material';

interface EditableCellProps {
  value: number;
  onSave: (newValue: number) => Promise<void>;
  type?: 'number' | 'text';
  min?: number;
  max?: number;
  validation?: (value: string) => string | null;
}

const EditableCell: React.FC<EditableCellProps> = ({
  value,
  onSave,
  type = 'number',
  min,
  max,
  validation,
}) => {
  const [isEditing, setIsEditing] = useState(false);
  const [editValue, setEditValue] = useState(value.toString());
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleEdit = () => {
    setIsEditing(true);
    setEditValue(value.toString());
    setError(null);
  };

  const handleCancel = () => {
    setIsEditing(false);
    setEditValue(value.toString());
    setError(null);
  };

  const handleSave = async () => {
    // Validate input
    if (validation) {
      const validationError = validation(editValue);
      if (validationError) {
        setError(validationError);
        return;
      }
    }

    const numValue = Number(editValue);
    if (type === 'number' && isNaN(numValue)) {
      setError('Please enter a valid number');
      return;
    }

    if (type === 'number' && min !== undefined && numValue < min) {
      setError(`Value must be at least ${min}`);
      return;
    }

    if (type === 'number' && max !== undefined && numValue > max) {
      setError(`Value must be at most ${max}`);
      return;
    }

    setIsLoading(true);
    setError(null);

    try {
      await onSave(numValue);
      setIsEditing(false);
    } catch (error) {
      setError('Failed to save changes');
    } finally {
      setIsLoading(false);
    }
  };

  const handleKeyPress = (event: React.KeyboardEvent) => {
    if (event.key === 'Enter') {
      handleSave();
    } else if (event.key === 'Escape') {
      handleCancel();
    }
  };

  if (!isEditing) {
    return (
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 0.5 }}>
        <Typography variant="body2">{value}</Typography>
        <Tooltip title="Click to edit">
          <IconButton size="small" onClick={handleEdit}>
            <EditIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      </Box>
    );
  }

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, minWidth: 120 }}>
      <TextField
        value={editValue}
        onChange={(e) => setEditValue(e.target.value)}
        onKeyDown={handleKeyPress}
        size="small"
        type={type}
        inputProps={{
          min: min,
          max: max,
          style: { textAlign: 'center' }
        }}
        error={!!error}
        helperText={error}
        disabled={isLoading}
        autoFocus
        sx={{ width: 80 }}
      />
      
      <Box sx={{ display: 'flex', flexDirection: 'column' }}>
        <Tooltip title="Save">
          <span>
            <IconButton
              size="small"
              onClick={handleSave}
              disabled={isLoading}
              color="primary"
            >
              {isLoading ? <CircularProgress size={16} /> : <CheckIcon fontSize="small" />}
            </IconButton>
          </span>
        </Tooltip>
        
        <Tooltip title="Cancel">
          <span>
            <IconButton
              size="small"
              onClick={handleCancel}
              disabled={isLoading}
              color="default"
            >
              <CloseIcon fontSize="small" />
            </IconButton>
          </span>
        </Tooltip>
      </Box>
    </Box>
  );
};

export default EditableCell;