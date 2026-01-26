/**
 * Rules Management page for KrakenHashes frontend.
 * 
 * Features:
 *   - View rules
 *   - Add new rules
 *   - Update rule information
 *   - Delete rules
 *   - Enable/disable rules
 */
import React, { useState, useEffect, useCallback } from 'react';
import {
  Box,
  Button,
  Typography,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TableSortLabel,
  IconButton,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  MenuItem,
  Grid,
  Divider,
  Switch,
  FormControlLabel,
  CircularProgress,
  Alert,
  Tooltip,
  InputAdornment,
  Toolbar,
  Tab,
  Tabs,
  Checkbox,
  FormControl,
  InputLabel,
  Select
} from '@mui/material';
import { 
  Delete as DeleteIcon, 
  Edit as EditIcon, 
  Refresh as RefreshIcon, 
  CloudDownload as DownloadIcon,
  Search as SearchIcon,
  Add as AddIcon,
  Check as CheckIcon,
  Clear as ClearIcon,
  Verified as VerifiedIcon
} from '@mui/icons-material';
import FileUpload from '../components/common/FileUpload';
import { Rule, RuleStatus, RuleType } from '../types/rules';
import { DeletionImpact } from '../types/wordlists';
import * as ruleService from '../services/rules';
import { useSnackbar } from 'notistack';
import { formatFileSize, formatAttackMode } from '../utils/formatters';

export default function RulesManagement() {
  const [rules, setRules] = useState<Rule[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [openUploadDialog, setOpenUploadDialog] = useState(false);
  const [openEditDialog, setOpenEditDialog] = useState(false);
  const [currentRule, setCurrentRule] = useState<Rule | null>(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [nameEdit, setNameEdit] = useState('');
  const [descriptionEdit, setDescriptionEdit] = useState('');
  const [ruleTypeEdit, setRuleTypeEdit] = useState<RuleType>(RuleType.HASHCAT);
  const [tabValue, setTabValue] = useState(0);
  const [sortBy, setSortBy] = useState<keyof Rule>('updated_at');
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc');
  const { enqueueSnackbar } = useSnackbar();
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false);
  const [selectedRuleType, setSelectedRuleType] = useState<RuleType>(RuleType.HASHCAT);
  const [isLoading, setIsLoading] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [ruleToDelete, setRuleToDelete] = useState<{id: string, name: string} | null>(null);
  const [deletionImpact, setDeletionImpact] = useState<DeletionImpact | null>(null);
  const [confirmationId, setConfirmationId] = useState('');
  const [isCheckingImpact, setIsCheckingImpact] = useState(false);

  // Fetch rules
  const fetchRules = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      
      const response = await ruleService.getRules();
      setRules(response.data);
    } catch (err) {
      console.error('Error fetching rules:', err);
      setError('Failed to load rules');
      enqueueSnackbar('Failed to load rules', { variant: 'error' });
    } finally {
      setLoading(false);
    }
  }, [enqueueSnackbar]);

  useEffect(() => {
    fetchRules();
  }, [fetchRules]);

  // Handle file upload
  const handleUploadRule = async (formData: FormData) => {
    try {
      setIsLoading(true);
      
      // Add the rule type to the form data
      formData.append('rule_type', selectedRuleType);
      
      // Add required fields if not present
      if (!formData.has('name')) {
        const file = formData.get('file') as File;
        if (file) {
          // Extract name without extension (everything before the last dot)
          const lastDotIndex = file.name.lastIndexOf('.');
          const nameWithoutExt = lastDotIndex > 0 ? file.name.substring(0, lastDotIndex) : file.name;
          formData.append('name', nameWithoutExt);
        }
      }
      
      // Remove format field as it's not needed for rules
      if (formData.has('format')) {
        formData.delete('format');
      }
      
      console.debug('[Rule Upload] Sending form data with rule_type:', selectedRuleType);
      console.debug('[Rule Upload] Form data contents:', 
        Array.from(formData.entries()).reduce((obj, [key, val]) => {
          obj[key] = key === 'file' ? '(file content)' : val;
          return obj;
        }, {} as Record<string, any>)
      );
      
      const response = await ruleService.uploadRule(formData, (progress, eta, speed) => {
        // Update progress in the FileUpload component
        const progressEvent = new CustomEvent('upload-progress', { detail: { progress, eta, speed } });
        document.dispatchEvent(progressEvent);
      });
      console.debug('[Rule Upload] Upload successful:', response);
      
      // Check if the response indicates a duplicate rule
      if (response.data.duplicate) {
        enqueueSnackbar(`Rule "${response.data.name}" already exists`, { variant: 'info' });
      } else {
        enqueueSnackbar('Rule uploaded successfully', { variant: 'success' });
      }
      
      setUploadDialogOpen(false);
      fetchRules();
    } catch (error) {
      console.error('Error uploading rule:', error);
      enqueueSnackbar('Failed to upload rule', { variant: 'error' });
    } finally {
      setIsLoading(false);
    }
  };

  // Handle rule deletion
  const handleDelete = async (id: string, name: string, confirmId?: number) => {
    try {
      await ruleService.deleteRule(id, confirmId);
      enqueueSnackbar(`Rule "${name}" deleted successfully`, { variant: 'success' });
      fetchRules();
    } catch (err: any) {
      console.error('Error deleting rule:', err);
      // Extract error message from axios response
      const errorMessage = err.response?.data?.error || 'Failed to delete rule';
      enqueueSnackbar(errorMessage, { variant: 'error' });
    } finally {
      closeDeleteDialog();
    }
  };

  // Open delete confirmation dialog - first check for deletion impact
  const openDeleteDialog = async (id: string, name: string) => {
    setRuleToDelete({ id, name });
    setDeleteDialogOpen(true);
    setIsCheckingImpact(true);
    setDeletionImpact(null);
    setConfirmationId('');

    try {
      const response = await ruleService.getRuleDeletionImpact(id);
      setDeletionImpact(response.data);
    } catch (err: any) {
      console.error('Error getting deletion impact:', err);
      // If we can't get the impact, still allow deletion with simple confirmation
      setDeletionImpact(null);
    } finally {
      setIsCheckingImpact(false);
    }
  };

  // Close delete confirmation dialog
  const closeDeleteDialog = () => {
    setDeleteDialogOpen(false);
    setRuleToDelete(null);
    setDeletionImpact(null);
    setConfirmationId('');
  };

  // Check if confirmation ID matches for cascade delete
  const isConfirmationValid = () => {
    if (!deletionImpact?.has_cascading_impact) return true;
    return confirmationId === String(deletionImpact.resource_id);
  };

  // Handle rule download
  const handleDownload = async (id: string, name: string) => {
    try {
      const response = await ruleService.downloadRule(id);
      
      // Create and trigger download
      const url = window.URL.createObjectURL(new Blob([response.data]));
      const link = document.createElement('a');
      link.href = url;
      link.setAttribute('download', `${name}.rule`);
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
    } catch (err) {
      console.error('Error downloading rule:', err);
      enqueueSnackbar('Failed to download rule', { variant: 'error' });
    }
  };

  // Handle edit button click
  const handleEditClick = (rule: Rule) => {
    setCurrentRule(rule);
    setNameEdit(rule.name);
    setDescriptionEdit(rule.description);
    setRuleTypeEdit(rule.rule_type);
    setOpenEditDialog(true);
  };

  // Handle save edit
  const handleSaveEdit = async () => {
    if (!currentRule) return;
    
    try {
      console.debug('[Rule Edit] Updating rule:', currentRule.id, {
        name: nameEdit,
        description: descriptionEdit,
        rule_type: ruleTypeEdit
      });
      
      const response = await ruleService.updateRule(currentRule.id, {
        name: nameEdit,
        description: descriptionEdit,
        rule_type: ruleTypeEdit
      });
      
      console.debug('[Rule Edit] Update successful:', response);
      enqueueSnackbar('Rule updated successfully', { variant: 'success' });
      setOpenEditDialog(false);
      fetchRules();
    } catch (err: any) {
      console.error('[Rule Edit] Error updating rule:', err);
      
      if (err.response?.status === 401) {
        enqueueSnackbar('Your session has expired. Please log in again.', { variant: 'error' });
      } else {
        enqueueSnackbar('Failed to update rule: ' + (err.response?.data?.message || err.message), { variant: 'error' });
      }
    }
  };

  // Handle rule verification
  const handleVerify = async (id: string, name: string) => {
    try {
      setIsLoading(true);
      await ruleService.verifyRule(id, 'verified');
      enqueueSnackbar(`Rule "${name}" verified successfully`, { variant: 'success' });
      fetchRules();
    } catch (err) {
      console.error('Error verifying rule:', err);
      enqueueSnackbar('Failed to verify rule', { variant: 'error' });
    } finally {
      setIsLoading(false);
    }
  };

  // Handle sort change
  const handleSortChange = (column: keyof Rule) => {
    if (sortBy === column) {
      // If already sorting by this column, toggle order
      setSortOrder(sortOrder === 'asc' ? 'desc' : 'asc');
    } else {
      // Otherwise, sort by this column in ascending order
      setSortBy(column);
      setSortOrder('asc');
    }
  };

  // Render sort label
  const renderSortLabel = (column: keyof Rule, label: string) => {
    return (
      <TableSortLabel
        active={sortBy === column}
        direction={sortBy === column ? sortOrder : 'asc'}
        onClick={() => handleSortChange(column)}
      >
        {label}
      </TableSortLabel>
    );
  };

  // Filter rules based on search term and tab
  const filteredRules = rules
    .filter(rule => {
      // Filter by search term
      const matchesSearch = rule.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
                           rule.description.toLowerCase().includes(searchTerm.toLowerCase());
      
      // Filter by tab
      if (tabValue === 0) return matchesSearch; // All
      if (tabValue === 1) return matchesSearch && rule.rule_type === RuleType.HASHCAT;
      if (tabValue === 2) return matchesSearch && rule.rule_type === RuleType.JOHN;
      
      return matchesSearch;
    })
    .sort((a, b) => {
      // Handle special cases for non-string fields
      if (sortBy === 'file_size' || sortBy === 'rule_count') {
        return sortOrder === 'asc' 
          ? a[sortBy] - b[sortBy] 
          : b[sortBy] - a[sortBy];
      }
      
      // Handle date fields
      if (sortBy === 'created_at' || sortBy === 'updated_at' || sortBy === 'last_verified_at') {
        const dateA = new Date(a[sortBy] || 0).getTime();
        const dateB = new Date(b[sortBy] || 0).getTime();
        return sortOrder === 'asc' ? dateA - dateB : dateB - dateA;
      }
      
      // Default string comparison
      const valueA = String(a[sortBy] || '').toLowerCase();
      const valueB = String(b[sortBy] || '').toLowerCase();
      return sortOrder === 'asc' 
        ? valueA.localeCompare(valueB) 
        : valueB.localeCompare(valueA);
    });

  return (
    <Box sx={{ p: 3 }}>
      <Grid container spacing={2} alignItems="center" sx={{ mb: 3 }}>
          <Grid item xs={12} sm={6}>
            <Typography variant="h4" component="h1" gutterBottom>
              Rule Management
            </Typography>
            <Typography variant="body1" color="text.secondary">
              Manage Hashcat rules for password cracking
            </Typography>
          </Grid>
          <Grid item xs={12} sm={6} sx={{ textAlign: { xs: 'left', sm: 'right' } }}>
            <Button
              variant="contained"
              startIcon={<AddIcon />}
              onClick={() => setUploadDialogOpen(true)}
              sx={{ mr: 1 }}
              disabled={isLoading}
            >
              Upload Rule
            </Button>
            <Button
              variant="outlined"
              startIcon={<RefreshIcon />}
              onClick={() => fetchRules()}
            >
              Refresh
            </Button>
          </Grid>
        </Grid>

        {error && (
          <Alert severity="error" sx={{ mb: 3 }}>
            {error}
          </Alert>
        )}

        <Paper sx={{ mb: 3, overflow: 'hidden' }}>
          <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
            <Tabs 
              value={tabValue} 
              onChange={(_, newValue) => setTabValue(newValue)}
              aria-label="rule tabs"
            >
              <Tab label="All Rules" id="tab-0" />
              <Tab label="Hashcat" id="tab-1" />
              <Tab label="John" id="tab-2" />
            </Tabs>
          </Box>
          
          <Toolbar
            sx={{
              pl: { sm: 2 },
              pr: { xs: 1, sm: 1 },
              display: 'flex',
              justifyContent: 'center'
            }}
          >
            <TextField
              margin="dense"
              placeholder="Search rules..."
              InputProps={{
                startAdornment: (
                  <InputAdornment position="start">
                    <SearchIcon />
                  </InputAdornment>
                ),
                endAdornment: searchTerm && (
                  <InputAdornment position="end">
                    <IconButton size="small" onClick={() => setSearchTerm('')}>
                      <ClearIcon />
                    </IconButton>
                  </InputAdornment>
                )
              }}
              size="small"
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              sx={{ width: { xs: '100%', sm: '60%', md: '40%' } }}
            />
          </Toolbar>
          
          <Divider />
          
          <TableContainer>
            <Table sx={{ minWidth: 650 }} aria-label="rules table">
              <TableHead>
                <TableRow>
                  <TableCell>
                    {renderSortLabel('name', 'Name')}
                  </TableCell>
                  <TableCell>
                    {renderSortLabel('verification_status', 'Status')}
                  </TableCell>
                  <TableCell>
                    {renderSortLabel('rule_type', 'Type')}
                  </TableCell>
                  <TableCell>
                    {renderSortLabel('file_size', 'Size')}
                  </TableCell>
                  <TableCell>
                    {renderSortLabel('rule_count', 'Rule Count')}
                  </TableCell>
                  <TableCell>
                    {renderSortLabel('updated_at', 'Updated')}
                  </TableCell>
                  <TableCell align="right">Actions</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={7} align="center" sx={{ py: 3 }}>
                      <CircularProgress size={40} />
                      <Typography variant="body2" sx={{ mt: 1 }}>
                        Loading rules...
                      </Typography>
                    </TableCell>
                  </TableRow>
                ) : filteredRules.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} align="center" sx={{ py: 3 }}>
                      <Typography variant="body1">
                        No rules found
                      </Typography>
                      <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                        {searchTerm ? 'Try a different search term' : 'Upload a rule to get started'}
                      </Typography>
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredRules.map((rule) => (
                    <TableRow key={rule.id}>
                      <TableCell>
                        <Box>
                          <Typography variant="body2" fontWeight="medium">
                            {rule.name}
                          </Typography>
                          <Typography variant="caption" color="text.secondary">
                            {rule.description || 'No description provided'}
                          </Typography>
                        </Box>
                      </TableCell>
                      <TableCell>
                        <Chip
                          label={rule.verification_status}
                          size="small"
                          color={
                            rule.verification_status === RuleStatus.READY
                              ? 'success'
                              : rule.verification_status === RuleStatus.PROCESSING
                              ? 'warning'
                              : 'error'
                          }
                          sx={{ textTransform: 'capitalize' }}
                        />
                      </TableCell>
                      <TableCell>
                        <Chip
                          label={rule.rule_type}
                          size="small"
                          color="primary"
                          variant="outlined"
                          sx={{ textTransform: 'capitalize' }}
                        />
                      </TableCell>
                      <TableCell>
                        {formatFileSize(rule.file_size)}
                      </TableCell>
                      <TableCell>
                        {rule.rule_count.toLocaleString()}
                      </TableCell>
                      <TableCell>
                        {new Date(rule.updated_at).toLocaleDateString()}
                      </TableCell>
                      <TableCell align="right">
                        <Tooltip title="Download">
                          <IconButton
                            onClick={() => handleDownload(rule.id, rule.name)}
                            disabled={rule.verification_status !== 'verified'}
                          >
                            <DownloadIcon />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="Edit">
                          <IconButton
                            onClick={() => handleEditClick(rule)}
                          >
                            <EditIcon />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="Delete">
                          <IconButton
                            color="error"
                            onClick={() => openDeleteDialog(rule.id, rule.name)}
                          >
                            <DeleteIcon />
                          </IconButton>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </TableContainer>
        </Paper>

      {/* Upload Dialog */}
      <Dialog
        open={uploadDialogOpen}
        onClose={() => setUploadDialogOpen(false)}
        maxWidth="md"
        fullWidth
      >
        <DialogTitle>Upload Rule</DialogTitle>
        <DialogContent>
          <FileUpload
            title="Upload a rule file"
            description="Select a rule file to upload. Supported formats: .rule, .rules, .txt"
            acceptedFileTypes=".rule,.rules,.txt,text/plain"
            onUpload={handleUploadRule}
            uploadButtonText="Upload Rule"
            additionalFields={
              <FormControl fullWidth margin="normal">
                <InputLabel id="rule-type-label">Rule Type</InputLabel>
                <Select
                  labelId="rule-type-label"
                  id="rule-type"
                  name="rule_type"
                  value={selectedRuleType}
                  onChange={(e) => setSelectedRuleType(e.target.value as RuleType)}
                  label="Rule Type"
                >
                  <MenuItem value={RuleType.HASHCAT}>Hashcat</MenuItem>
                  <MenuItem value={RuleType.JOHN}>John the Ripper</MenuItem>
                </Select>
              </FormControl>
            }
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setUploadDialogOpen(false)} color="primary">
            Cancel
          </Button>
        </DialogActions>
      </Dialog>

      {/* Edit Dialog */}
      <Dialog
        open={openEditDialog}
        onClose={() => setOpenEditDialog(false)}
        aria-labelledby="edit-dialog-title"
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle id="edit-dialog-title">Edit Rule</DialogTitle>
        <DialogContent>
          <TextField
            margin="dense"
            label="Name"
            fullWidth
            value={nameEdit}
            onChange={(e) => setNameEdit(e.target.value)}
            sx={{ mb: 2 }}
          />
          <TextField
            margin="dense"
            label="Description"
            fullWidth
            multiline
            rows={3}
            value={descriptionEdit}
            onChange={(e) => setDescriptionEdit(e.target.value)}
            sx={{ mb: 2 }}
          />
          <FormControl fullWidth margin="dense" sx={{ mb: 2 }}>
            <InputLabel id="edit-rule-type-label">Rule Type</InputLabel>
            <Select
              labelId="edit-rule-type-label"
              id="edit-rule-type"
              value={ruleTypeEdit}
              onChange={(e) => setRuleTypeEdit(e.target.value as RuleType)}
              label="Rule Type"
            >
              <MenuItem value={RuleType.HASHCAT}>Hashcat</MenuItem>
              <MenuItem value={RuleType.JOHN}>John the Ripper</MenuItem>
            </Select>
          </FormControl>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setOpenEditDialog(false)}>
            Cancel
          </Button>
          <Button onClick={handleSaveEdit} variant="contained" color="primary">
            Save Changes
          </Button>
        </DialogActions>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <Dialog
        open={deleteDialogOpen}
        onClose={closeDeleteDialog}
        aria-labelledby="delete-dialog-title"
        aria-describedby="delete-dialog-description"
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle id="delete-dialog-title">
          {deletionImpact?.has_cascading_impact ? 'Confirm Cascade Deletion' : 'Confirm Deletion'}
        </DialogTitle>
        <DialogContent>
          {isCheckingImpact ? (
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', py: 3 }}>
              <CircularProgress size={24} sx={{ mr: 2 }} />
              <Typography>Checking for dependencies...</Typography>
            </Box>
          ) : deletionImpact?.has_cascading_impact ? (
            <Box>
              <Alert severity="warning" sx={{ mb: 2 }}>
                Deleting rule "{ruleToDelete?.name}" will also delete the following:
              </Alert>

              {deletionImpact.summary.total_jobs > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {deletionImpact.summary.total_jobs} Job(s) (pending/running/failed):
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.jobs.slice(0, 5).map((job) => (
                      <li key={job.id}>
                        <Typography variant="body2" color="text.secondary">
                          {job.name} ({job.status}) - {job.hashlist_name || 'No hashlist'}
                        </Typography>
                      </li>
                    ))}
                    {deletionImpact.summary.total_jobs > 5 && (
                      <li>
                        <Typography variant="body2" color="text.secondary">
                          ...and {deletionImpact.summary.total_jobs - 5} more
                        </Typography>
                      </li>
                    )}
                  </Box>
                </Box>
              )}

              {deletionImpact.summary.total_preset_jobs > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {deletionImpact.summary.total_preset_jobs} Preset Job(s):
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.preset_jobs.slice(0, 5).map((pj) => (
                      <li key={pj.id}>
                        <Typography variant="body2" color="text.secondary">
                          {pj.name} ({formatAttackMode(pj.attack_mode)})
                        </Typography>
                      </li>
                    ))}
                    {deletionImpact.summary.total_preset_jobs > 5 && (
                      <li>
                        <Typography variant="body2" color="text.secondary">
                          ...and {deletionImpact.summary.total_preset_jobs - 5} more
                        </Typography>
                      </li>
                    )}
                  </Box>
                </Box>
              )}

              {deletionImpact.summary.total_workflow_steps > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {deletionImpact.summary.total_workflow_steps} Workflow Step(s):
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.workflow_steps.slice(0, 5).map((step, idx) => (
                      <li key={`${step.workflow_id}-${step.step_order}-${idx}`}>
                        <Typography variant="body2" color="text.secondary">
                          {step.workflow_name} → Step {step.step_order} ({step.preset_job_name})
                        </Typography>
                      </li>
                    ))}
                    {deletionImpact.summary.total_workflow_steps > 5 && (
                      <li>
                        <Typography variant="body2" color="text.secondary">
                          ...and {deletionImpact.summary.total_workflow_steps - 5} more
                        </Typography>
                      </li>
                    )}
                  </Box>
                </Box>
              )}

              {deletionImpact.summary.total_workflows_to_delete > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {deletionImpact.summary.total_workflows_to_delete} Empty Workflow(s) will be deleted:
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.workflows_to_delete.map((wf) => (
                      <li key={wf.id}>
                        <Typography variant="body2" color="text.secondary">
                          {wf.name}
                        </Typography>
                      </li>
                    ))}
                  </Box>
                </Box>
              )}

              <Divider sx={{ my: 2 }} />

              <Typography variant="body2" sx={{ mb: 1 }}>
                To confirm this cascade deletion, type the rule ID: <strong>{deletionImpact.resource_id}</strong>
              </Typography>
              <TextField
                fullWidth
                size="small"
                placeholder={`Type ${deletionImpact.resource_id} to confirm`}
                value={confirmationId}
                onChange={(e) => setConfirmationId(e.target.value)}
                error={confirmationId !== '' && !isConfirmationValid()}
                helperText={confirmationId !== '' && !isConfirmationValid() ? 'ID does not match' : ''}
              />
            </Box>
          ) : (
            <Typography variant="body1" id="delete-dialog-description">
              Are you sure you want to delete rule "{ruleToDelete?.name}"? This action cannot be undone.
            </Typography>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={closeDeleteDialog}>Cancel</Button>
          <Button
            onClick={() => {
              if (ruleToDelete) {
                const confirmId = deletionImpact?.has_cascading_impact ? Number(confirmationId) : undefined;
                handleDelete(ruleToDelete.id, ruleToDelete.name, confirmId);
              }
            }}
            color="error"
            variant="contained"
            disabled={isCheckingImpact || (deletionImpact?.has_cascading_impact && !isConfirmationValid())}
          >
            {deletionImpact?.has_cascading_impact ? 'Delete All' : 'Delete'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
} 