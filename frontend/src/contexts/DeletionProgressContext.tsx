import React, { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react';
import { useSnackbar } from 'notistack';
import { useQueryClient } from '@tanstack/react-query';
import { getDeletionProgress, DeletionProgressResponse } from '../services/api';

export interface DeletionEntry {
  hashlistId: string;
  hashlistName: string;
  status: 'pending' | 'deleting_hashes' | 'clearing_references' | 'cleaning_orphans' | 'finalizing' | 'completed' | 'failed';
  progress: DeletionProgressResponse | null;
  startedAt: Date;
  error?: string;
}

// Bulk deletion queue entry
interface BulkQueueItem {
  hashlistId: string;
  hashlistName: string;
  status: 'queued' | 'deleting' | 'completed' | 'failed';
  error?: string;
}

interface BulkDeletionState {
  items: BulkQueueItem[];
  isRunning: boolean;
  currentIndex: number;
  deleteFunction: ((hashlistId: string) => Promise<{ async: boolean }>) | null;
}

interface DeletionProgressContextType {
  activeDeletions: Map<string, DeletionEntry>;
  startTracking: (hashlistId: string, hashlistName: string) => void;
  isDeleting: (hashlistId: string) => boolean;
  getDeletion: (hashlistId: string) => DeletionEntry | undefined;
  bulkDeletion: BulkDeletionState;
  startBulkDeletion: (
    items: Array<{ id: string; name: string }>,
    deleteFunction: (hashlistId: string) => Promise<{ async: boolean }>
  ) => void;
  isBulkDeleting: boolean;
  activeCount: number;
}

const DeletionProgressContext = createContext<DeletionProgressContextType | undefined>(undefined);

export function DeletionProgressProvider({ children }: { children: React.ReactNode }) {
  const { enqueueSnackbar } = useSnackbar();
  const queryClient = useQueryClient();
  const [activeDeletions, setActiveDeletions] = useState<Map<string, DeletionEntry>>(new Map());
  const [bulkDeletion, setBulkDeletion] = useState<BulkDeletionState>({
    items: [],
    isRunning: false,
    currentIndex: -1,
    deleteFunction: null,
  });
  const pollingRef = useRef<NodeJS.Timeout | null>(null);
  const bulkProcessingRef = useRef(false);
  // Ref to avoid stale closure in polling interval callback
  const activeDeletionsRef = useRef(activeDeletions);
  const enqueueSnackbarRef = useRef(enqueueSnackbar);

  // Keep refs in sync with latest values
  useEffect(() => { activeDeletionsRef.current = activeDeletions; }, [activeDeletions]);
  useEffect(() => { enqueueSnackbarRef.current = enqueueSnackbar; }, [enqueueSnackbar]);

  // Derive whether we have active (non-terminal) entries
  const hasActiveEntries = Array.from(activeDeletions.values()).some(
    e => e.status !== 'completed' && e.status !== 'failed'
  );

  // Poll all active deletions every 2 seconds
  // Only start/stop based on whether there ARE active entries, not on the map itself
  useEffect(() => {
    if (!hasActiveEntries) {
      if (pollingRef.current) {
        clearInterval(pollingRef.current);
        pollingRef.current = null;
      }
      return;
    }

    if (pollingRef.current) return; // Already polling

    const poll = async () => {
      // Read from ref to get latest state (avoids stale closure)
      const currentDeletions = new Map(activeDeletionsRef.current);
      let hasChanges = false;

      const entries = Array.from(currentDeletions.entries());
      for (let idx = 0; idx < entries.length; idx++) {
        const [id, entry] = entries[idx];
        if (entry.status === 'completed' || entry.status === 'failed') continue;

        try {
          const progress = await getDeletionProgress(id);
          const updatedEntry: DeletionEntry = {
            ...entry,
            status: progress.status as DeletionEntry['status'],
            progress,
            error: progress.error,
          };

          if (progress.status === 'completed') {
            enqueueSnackbarRef.current(`Hashlist "${entry.hashlistName}" deleted successfully`, { variant: 'success' });
            queryClient.invalidateQueries({ queryKey: ['hashlists'] });
          } else if (progress.status === 'failed') {
            enqueueSnackbarRef.current(`Failed to delete "${entry.hashlistName}": ${progress.error || 'Unknown error'}`, { variant: 'error' });
          }

          currentDeletions.set(id, updatedEntry);
          hasChanges = true;
        } catch (error: any) {
          if (error.response?.status === 404) {
            // Progress cleaned up = deletion completed
            const updatedEntry: DeletionEntry = { ...entry, status: 'completed', progress: null };
            currentDeletions.set(id, updatedEntry);
            enqueueSnackbarRef.current(`Hashlist "${entry.hashlistName}" deleted successfully`, { variant: 'success' });
            queryClient.invalidateQueries({ queryKey: ['hashlists'] });
            hasChanges = true;
          }
        }
      }

      if (hasChanges) {
        setActiveDeletions(new Map(currentDeletions));
      }
    };

    poll(); // Immediate first poll
    pollingRef.current = setInterval(poll, 2000);

    return () => {
      if (pollingRef.current) {
        clearInterval(pollingRef.current);
        pollingRef.current = null;
      }
    };
  }, [hasActiveEntries, queryClient]);

  // Clean up completed/failed entries after 5 seconds
  // But don't clean up entries that are part of an active bulk operation
  useEffect(() => {
    const bulkIds = new Set(
      bulkDeletion.isRunning ? bulkDeletion.items.map(i => i.hashlistId) : []
    );
    const completedEntries = Array.from(activeDeletions.entries()).filter(
      ([id, e]) => (e.status === 'completed' || e.status === 'failed') && !bulkIds.has(id)
    );

    if (completedEntries.length === 0) return;

    const timeout = setTimeout(() => {
      setActiveDeletions(prev => {
        const next = new Map(prev);
        for (const [id, entry] of completedEntries) {
          if (entry.status === 'completed' || entry.status === 'failed') {
            next.delete(id);
          }
        }
        return next;
      });
    }, 5000);

    return () => clearTimeout(timeout);
  }, [activeDeletions, bulkDeletion.isRunning, bulkDeletion.items]);

  // Process bulk deletion queue
  useEffect(() => {
    if (!bulkDeletion.isRunning || bulkProcessingRef.current) return;
    if (bulkDeletion.currentIndex < 0 || bulkDeletion.currentIndex >= bulkDeletion.items.length) return;

    const currentItem = bulkDeletion.items[bulkDeletion.currentIndex];
    if (currentItem.status !== 'queued') return;
    if (!bulkDeletion.deleteFunction) return; // Guard against null

    bulkProcessingRef.current = true;
    const deleteFn = bulkDeletion.deleteFunction;

    const processItem = async () => {
      // Mark as deleting
      setBulkDeletion(prev => ({
        ...prev,
        items: prev.items.map((item, i) =>
          i === prev.currentIndex ? { ...item, status: 'deleting' as const } : item
        ),
      }));

      try {
        const result = await deleteFn(currentItem.hashlistId);

        if (result.async) {
          // Async deletion — track it and wait for completion before advancing
          startTracking(currentItem.hashlistId, currentItem.hashlistName);
          // We'll advance in the polling effect when this entry completes
        } else {
          // Sync deletion completed immediately
          setBulkDeletion(prev => ({
            ...prev,
            items: prev.items.map((item, i) =>
              i === prev.currentIndex ? { ...item, status: 'completed' as const } : item
            ),
            currentIndex: prev.currentIndex + 1,
          }));
          queryClient.invalidateQueries({ queryKey: ['hashlists'] });
        }
      } catch (error: any) {
        const errorMsg = error.response?.data?.error || error.message || 'Unknown error';
        enqueueSnackbar(`Failed to delete "${currentItem.hashlistName}": ${errorMsg}`, { variant: 'error' });
        setBulkDeletion(prev => ({
          ...prev,
          items: prev.items.map((item, i) =>
            i === prev.currentIndex ? { ...item, status: 'failed' as const, error: errorMsg } : item
          ),
          currentIndex: prev.currentIndex + 1,
        }));
      } finally {
        bulkProcessingRef.current = false;
      }
    };

    processItem();
  }, [bulkDeletion.isRunning, bulkDeletion.currentIndex, bulkDeletion.items, bulkDeletion.deleteFunction, enqueueSnackbar, queryClient]);

  // Advance bulk queue when an async deletion completes (or entry was cleaned up)
  useEffect(() => {
    if (!bulkDeletion.isRunning) return;
    if (bulkDeletion.currentIndex < 0 || bulkDeletion.currentIndex >= bulkDeletion.items.length) return;

    const currentItem = bulkDeletion.items[bulkDeletion.currentIndex];
    if (currentItem.status !== 'deleting') return;

    // Check if this item's async deletion has completed
    const entry = activeDeletions.get(currentItem.hashlistId);

    // If entry doesn't exist, it was already cleaned up — treat as completed
    const entryStatus = entry ? entry.status : 'completed';

    if (entryStatus === 'completed') {
      setBulkDeletion(prev => ({
        ...prev,
        items: prev.items.map((item, i) =>
          i === prev.currentIndex ? { ...item, status: 'completed' as const } : item
        ),
        currentIndex: prev.currentIndex + 1,
      }));
      bulkProcessingRef.current = false;
    } else if (entryStatus === 'failed') {
      setBulkDeletion(prev => ({
        ...prev,
        items: prev.items.map((item, i) =>
          i === prev.currentIndex ? { ...item, status: 'failed' as const, error: entry?.error } : item
        ),
        currentIndex: prev.currentIndex + 1,
      }));
      bulkProcessingRef.current = false;
    }
  }, [activeDeletions, bulkDeletion]);

  // Detect bulk deletion completion
  useEffect(() => {
    if (!bulkDeletion.isRunning || bulkDeletion.items.length === 0) return;
    if (bulkDeletion.currentIndex < bulkDeletion.items.length) return;

    // All items processed
    const successCount = bulkDeletion.items.filter(i => i.status === 'completed').length;
    const failedCount = bulkDeletion.items.filter(i => i.status === 'failed').length;

    if (successCount > 0) {
      enqueueSnackbar(`${successCount} hashlist(s) deleted successfully${failedCount > 0 ? `, ${failedCount} failed` : ''}`, { variant: successCount > 0 && failedCount === 0 ? 'success' : 'warning' });
    }

    setBulkDeletion(prev => ({ ...prev, isRunning: false }));
  }, [bulkDeletion.currentIndex, bulkDeletion.items, bulkDeletion.isRunning, enqueueSnackbar]);

  // Clean up bulk state after completion
  useEffect(() => {
    if (bulkDeletion.isRunning || bulkDeletion.items.length === 0) return;
    const timeout = setTimeout(() => {
      setBulkDeletion({ items: [], isRunning: false, currentIndex: -1, deleteFunction: null });
    }, 5000);
    return () => clearTimeout(timeout);
  }, [bulkDeletion.isRunning, bulkDeletion.items.length]);

  const startTracking = useCallback((hashlistId: string, hashlistName: string) => {
    setActiveDeletions(prev => {
      const next = new Map(prev);
      next.set(hashlistId, {
        hashlistId,
        hashlistName,
        status: 'pending',
        progress: null,
        startedAt: new Date(),
      });
      return next;
    });
    // Force restart polling by clearing current interval
    // The effect will restart it on next render since hasActiveEntries will be true
    if (pollingRef.current) {
      clearInterval(pollingRef.current);
      pollingRef.current = null;
    }
  }, []);

  const isDeleting = useCallback((hashlistId: string) => {
    const entry = activeDeletions.get(hashlistId);
    if (entry && entry.status !== 'completed' && entry.status !== 'failed') return true;
    // Also check bulk queue
    return bulkDeletion.items.some(item => item.hashlistId === hashlistId && (item.status === 'queued' || item.status === 'deleting'));
  }, [activeDeletions, bulkDeletion.items]);

  const getDeletion = useCallback((hashlistId: string) => {
    return activeDeletions.get(hashlistId);
  }, [activeDeletions]);

  const startBulkDeletion = useCallback((
    items: Array<{ id: string; name: string }>,
    deleteFunction: (hashlistId: string) => Promise<{ async: boolean }>
  ) => {
    const queueItems: BulkQueueItem[] = items.map(item => ({
      hashlistId: item.id,
      hashlistName: item.name,
      status: 'queued' as const,
    }));
    setBulkDeletion({
      items: queueItems,
      isRunning: true,
      currentIndex: 0,
      deleteFunction,
    });
  }, []);

  const activeCount = Array.from(activeDeletions.values()).filter(
    e => e.status !== 'completed' && e.status !== 'failed'
  ).length;

  const value: DeletionProgressContextType = {
    activeDeletions,
    startTracking,
    isDeleting,
    getDeletion,
    bulkDeletion,
    startBulkDeletion,
    isBulkDeleting: bulkDeletion.isRunning,
    activeCount,
  };

  return (
    <DeletionProgressContext.Provider value={value}>
      {children}
    </DeletionProgressContext.Provider>
  );
}

export function useDeletionProgress() {
  const context = useContext(DeletionProgressContext);
  if (!context) {
    throw new Error('useDeletionProgress must be used within a DeletionProgressProvider');
  }
  return context;
}
