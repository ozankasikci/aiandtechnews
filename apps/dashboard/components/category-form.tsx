"use client";

import { useState, useEffect } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { slugify } from "@/lib/utils";

const presetColors = [
  "#8b5cf6",
  "#3b82f6",
  "#10b981",
  "#f59e0b",
  "#ec4899",
  "#6366f1",
  "#ef4444",
  "#06b6d4",
];

interface CategoryFormData {
  name: string;
  slug: string;
  description: string;
  color: string;
}

interface CategoryFormProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initialData?: CategoryFormData;
  onSubmit: (data: CategoryFormData) => void;
  title: string;
}

export function CategoryForm({
  open,
  onOpenChange,
  initialData,
  onSubmit,
  title,
}: CategoryFormProps) {
  const [name, setName] = useState(initialData?.name ?? "");
  const [slug, setSlug] = useState(initialData?.slug ?? "");
  const [description, setDescription] = useState(
    initialData?.description ?? ""
  );
  const [color, setColor] = useState(initialData?.color ?? presetColors[0]);
  const [customColor, setCustomColor] = useState("");

  useEffect(() => {
    if (initialData) {
      setName(initialData.name);
      setSlug(initialData.slug);
      setDescription(initialData.description);
      setColor(initialData.color);
    } else {
      setName("");
      setSlug("");
      setDescription("");
      setColor(presetColors[0]);
    }
    setCustomColor("");
  }, [initialData, open]);

  const handleNameChange = (value: string) => {
    setName(value);
    if (!initialData) {
      setSlug(slugify(value));
    }
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSubmit({ name, slug, description, color });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="cat-name">Name</Label>
            <Input
              id="cat-name"
              value={name}
              onChange={(e) => handleNameChange(e.target.value)}
              placeholder="Category name"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="cat-slug">Slug</Label>
            <Input
              id="cat-slug"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              placeholder="category-slug"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="cat-desc">Description</Label>
            <Textarea
              id="cat-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Brief description of this category"
              rows={3}
            />
          </div>
          <div className="space-y-2">
            <Label>Color</Label>
            <div className="flex flex-wrap gap-2">
              {presetColors.map((c) => (
                <button
                  key={c}
                  type="button"
                  onClick={() => setColor(c)}
                  className={`h-8 w-8 rounded-full border-2 transition-all cursor-pointer ${
                    color === c
                      ? "border-white scale-110"
                      : "border-transparent hover:scale-105"
                  }`}
                  style={{ backgroundColor: c }}
                />
              ))}
            </div>
            <div className="flex items-center gap-2 mt-2">
              <Label htmlFor="hex-input" className="text-xs text-muted-foreground shrink-0">
                Custom:
              </Label>
              <Input
                id="hex-input"
                value={customColor}
                onChange={(e) => {
                  setCustomColor(e.target.value);
                  if (/^#[0-9a-fA-F]{6}$/.test(e.target.value)) {
                    setColor(e.target.value);
                  }
                }}
                placeholder="#6366f1"
                className="h-8 text-xs"
              />
              <div
                className="h-8 w-8 shrink-0 rounded-full border border-[var(--border-secondary)]"
                style={{ backgroundColor: color }}
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button type="submit">Save</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
