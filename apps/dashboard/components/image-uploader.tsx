"use client";

import { useState, useCallback } from "react";
import { Upload, X, Image as ImageIcon } from "lucide-react";

interface ImageUploaderProps {
  value?: string | null;
  onChange?: (url: string | null) => void;
}

export function ImageUploader({ value, onChange }: ImageUploaderProps) {
  const [isDragging, setIsDragging] = useState(false);

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(true);
  }, []);

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setIsDragging(false);
      // Mock: just set a placeholder
      onChange?.("/placeholder/800/450/6366f1");
    },
    [onChange]
  );

  const handleClick = useCallback(() => {
    // Mock: simulate file pick
    onChange?.("/placeholder/800/450/6366f1");
  }, [onChange]);

  if (value) {
    return (
      <div className="relative rounded-lg border border-dashed border-[var(--border-secondary)] overflow-hidden group">
        <div
          className="w-full aspect-video flex items-center justify-center"
          style={{ backgroundColor: "#6366f120" }}
        >
          <ImageIcon className="h-12 w-12 text-[var(--accent-primary)] opacity-50" />
        </div>
        <button
          type="button"
          onClick={() => onChange?.(null)}
          className="absolute top-2 right-2 rounded-full bg-black/60 p-1 opacity-0 group-hover:opacity-100 transition-opacity"
        >
          <X className="h-4 w-4 text-white" />
        </button>
      </div>
    );
  }

  return (
    <div
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      onClick={handleClick}
      className={`
        flex flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed p-8 cursor-pointer transition-colors
        ${
          isDragging
            ? "border-[var(--accent-primary)] bg-[var(--accent-muted)]"
            : "border-[var(--border-secondary)] hover:border-[var(--border-focus)] hover:bg-[var(--bg-hover)]"
        }
      `}
    >
      <Upload className="h-8 w-8 text-[var(--text-tertiary)]" />
      <div className="text-center">
        <p className="text-sm font-medium text-[var(--text-secondary)]">
          Drop an image here or click to upload
        </p>
        <p className="text-xs text-[var(--text-tertiary)] mt-1">
          PNG, JPG, WebP up to 5MB
        </p>
      </div>
    </div>
  );
}
