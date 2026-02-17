"use client";

import { useState, useCallback, useEffect } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { showToast } from "@/components/toast";
import { mediaApi } from "@/lib/api";
import { formatDate, formatFileSize } from "@/lib/utils";
import { Upload, Trash2, X, Image as ImageIcon, Loader2 } from "lucide-react";
import type { Media } from "@technews/shared";

export default function MediaPage() {
  const [media, setMedia] = useState<Media[]>([]);
  const [previewItem, setPreviewItem] = useState<Media | null>(null);
  const [deleteId, setDeleteId] = useState<number | null>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [loading, setLoading] = useState(true);
  const [uploading, setUploading] = useState(false);

  // Fetch media on mount
  useEffect(() => {
    const fetchMedia = async () => {
      setLoading(true);
      try {
        const { media } = await mediaApi.list();
        setMedia(media);
      } catch (error) {
        console.error('Failed to fetch media:', error);
        showToast("Failed to load media", "error");
      } finally {
        setLoading(false);
      }
    };
    
    fetchMedia();
  }, []);

  const handleFileUpload = async (files: FileList) => {
    if (files.length === 0) return;
    
    setUploading(true);
    try {
      const uploadPromises = Array.from(files).map(async (file) => {
        // Validate file type
        if (!file.type.startsWith('image/')) {
          showToast(`${file.name} is not an image file`, "error");
          return null;
        }
        
        // Validate file size (max 10MB)
        if (file.size > 10 * 1024 * 1024) {
          showToast(`${file.name} is too large (max 10MB)`, "error");
          return null;
        }
        
        const { media } = await mediaApi.upload(file);
        return media;
      });
      
      const uploadedMedia = (await Promise.all(uploadPromises)).filter(Boolean) as Media[];
      setMedia((prev) => [...uploadedMedia, ...prev]);
      
      if (uploadedMedia.length > 0) {
        showToast(`${uploadedMedia.length} file(s) uploaded successfully`, "success");
      }
    } catch (error: any) {
      console.error('Failed to upload files:', error);
      showToast(error.message || "Failed to upload files", "error");
    } finally {
      setUploading(false);
    }
  };

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(true);
  }, []);

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
  }, []);

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
    const files = e.dataTransfer.files;
    handleFileUpload(files);
  }, []);

  const handleFileInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (files) {
      handleFileUpload(files);
    }
    e.target.value = ''; // Reset input
  };

  const handleDelete = async () => {
    if (deleteId !== null) {
      try {
        await mediaApi.delete(deleteId);
        setMedia((prev) => prev.filter((m) => m.id !== deleteId));
        setDeleteId(null);
        showToast("File deleted successfully", "success");
      } catch (error: any) {
        console.error('Failed to delete media:', error);
        showToast(error.message || "Failed to delete file", "error");
        setDeleteId(null);
      }
    }
  };

  const getImageUrl = (media: Media) => {
    if (media.url.startsWith('/uploads/')) {
      return `${media.url}`;
    }
    return media.url;
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="text-center">
          <Loader2 className="h-8 w-8 animate-spin mx-auto mb-4" />
          <p className="text-sm text-[var(--text-secondary)]">Loading media...</p>
        </div>
      </div>
    );
  }

  return (
    <div>
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-bold text-[var(--text-primary)]">Media</h2>
        <div>
          <input
            type="file"
            id="file-upload"
            multiple
            accept="image/*"
            className="hidden"
            onChange={handleFileInputChange}
          />
          <Button
            className="bg-[var(--accent-primary)] hover:bg-[var(--accent-hover)] text-white"
            onClick={() => document.getElementById('file-upload')?.click()}
            disabled={uploading}
          >
            {uploading ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Uploading...
              </>
            ) : (
              <>
                <Upload className="h-4 w-4 mr-2" />
                Upload Files
              </>
            )}
          </Button>
        </div>
      </div>

      {/* Upload area */}
      <div
        className={`
          relative rounded-lg border-2 border-dashed p-8 mb-8 transition-colors
          ${
            isDragging
              ? "border-[var(--accent-primary)] bg-[var(--accent-primary)]/5"
              : "border-[var(--border-primary)] hover:border-[var(--accent-primary)]/50"
          }
        `}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
      >
        <div className="flex flex-col items-center text-center">
          <Upload className="h-12 w-12 text-[var(--text-tertiary)] mb-4" />
          <h3 className="text-lg font-medium text-[var(--text-primary)] mb-2">
            Drop files here to upload
          </h3>
          <p className="text-sm text-[var(--text-tertiary)] mb-4">
            Or click the button above to select files
          </p>
          <p className="text-xs text-[var(--text-tertiary)]">
            Supports: JPG, PNG, GIF, WebP • Max size: 10MB each
          </p>
        </div>
      </div>

      {/* Media grid */}
      {media.length === 0 ? (
        <div className="text-center py-12">
          <ImageIcon className="h-12 w-12 text-[var(--text-tertiary)] mx-auto mb-4" />
          <p className="text-sm text-[var(--text-tertiary)]">No media files found.</p>
        </div>
      ) : (
        <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-4">
          {media.map((item) => (
            <div
              key={item.id}
              className="group relative aspect-square rounded-lg overflow-hidden border border-[var(--border-primary)] hover:border-[var(--accent-primary)] transition-colors cursor-pointer"
              onClick={() => setPreviewItem(item)}
            >
              <img
                src={getImageUrl(item)}
                alt={item.filename}
                className="w-full h-full object-cover group-hover:scale-105 transition-transform"
              />
              <div className="absolute inset-0 bg-black/60 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center">
                <Button
                  size="icon"
                  variant="ghost"
                  className="text-white hover:text-white hover:bg-red-500/20 absolute top-2 right-2"
                  onClick={(e) => {
                    e.stopPropagation();
                    setDeleteId(item.id);
                  }}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
              <div className="absolute bottom-0 left-0 right-0 bg-gradient-to-t from-black/80 to-transparent p-2">
                <p className="text-white text-xs font-medium truncate">
                  {item.filename}
                </p>
                <p className="text-white/70 text-xs">
                  {formatFileSize(item.size)}
                </p>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Preview dialog */}
      <Dialog
        open={!!previewItem}
        onOpenChange={(open) => {
          if (!open) setPreviewItem(null);
        }}
      >
        <DialogContent className="max-w-4xl">
          <DialogHeader>
            <div className="flex items-center justify-between">
              <DialogTitle>{previewItem?.filename}</DialogTitle>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => setPreviewItem(null)}
              >
                <X className="h-4 w-4" />
              </Button>
            </div>
          </DialogHeader>
          {previewItem && (
            <div className="space-y-4">
              <img
                src={getImageUrl(previewItem)}
                alt={previewItem.filename}
                className="w-full h-auto max-h-[60vh] object-contain rounded-lg"
              />
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <strong>File size:</strong> {formatFileSize(previewItem.size)}
                </div>
                <div>
                  <strong>Type:</strong> {previewItem.mime_type}
                </div>
                <div>
                  <strong>Uploaded:</strong> {formatDate(previewItem.uploaded_at)}
                </div>
                <div>
                  <strong>URL:</strong>
                  <code className="ml-1 text-xs bg-[var(--bg-hover)] px-1 py-0.5 rounded">
                    {previewItem.url}
                  </code>
                </div>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      {/* Delete confirmation */}
      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteId(null);
        }}
        title="Delete File"
        description="Are you sure you want to delete this file? This action cannot be undone."
        confirmLabel="Delete"
        onConfirm={handleDelete}
        variant="destructive"
      />
    </div>
  );
}