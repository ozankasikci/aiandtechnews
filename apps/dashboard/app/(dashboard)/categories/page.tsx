"use client";

import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CategoryForm } from "@/components/category-form";
import { showToast } from "@/components/toast";
import { categoriesApi } from "@/lib/api";
import { truncate } from "@/lib/utils";
import { Plus, MoreHorizontal, Pencil, Trash2, Loader2 } from "lucide-react";
import type { Category } from "@technews/shared";

type CategoryWithCount = Category & { article_count?: number };

export default function CategoriesPage() {
  const [categories, setCategories] = useState<CategoryWithCount[]>([]);
  const [deleteId, setDeleteId] = useState<number | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [editingCategory, setEditingCategory] = useState<Category | null>(null);
  const [loading, setLoading] = useState(true);

  // Fetch categories on mount
  useEffect(() => {
    const fetchCategories = async () => {
      setLoading(true);
      try {
        const { categories } = await categoriesApi.list();
        setCategories(categories);
      } catch (error) {
        console.error('Failed to fetch categories:', error);
        showToast("Failed to load categories", "error");
      } finally {
        setLoading(false);
      }
    };
    
    fetchCategories();
  }, []);

  const handleCreate = async (data: {
    name: string;
    slug: string;
    description: string;
    color: string;
  }) => {
    try {
      const { category } = await categoriesApi.create(data);
      setCategories((prev) => [...prev, { ...category, article_count: 0 }]);
      setFormOpen(false);
      showToast("Category created successfully", "success");
    } catch (error: any) {
      console.error('Failed to create category:', error);
      showToast(error.message || "Failed to create category", "error");
    }
  };

  const handleEdit = async (data: {
    name: string;
    slug: string;
    description: string;
    color: string;
  }) => {
    if (!editingCategory) return;

    try {
      const { category } = await categoriesApi.update(editingCategory.id, data);
      setCategories((prev) =>
        prev.map((c) => (c.id === editingCategory.id ? { ...c, ...category } : c))
      );
      setEditingCategory(null);
      showToast("Category updated successfully", "success");
    } catch (error: any) {
      console.error('Failed to update category:', error);
      showToast(error.message || "Failed to update category", "error");
    }
  };

  const handleDelete = async () => {
    if (deleteId !== null) {
      try {
        await categoriesApi.delete(deleteId);
        setCategories((prev) => prev.filter((c) => c.id !== deleteId));
        setDeleteId(null);
        showToast("Category deleted successfully", "success");
      } catch (error: any) {
        console.error('Failed to delete category:', error);
        showToast(error.message || "Failed to delete category", "error");
        setDeleteId(null);
      }
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="text-center">
          <Loader2 className="h-8 w-8 animate-spin mx-auto mb-4" />
          <p className="text-sm text-[var(--text-secondary)]">Loading categories...</p>
        </div>
      </div>
    );
  }

  return (
    <div>
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-bold text-[var(--text-primary)]">
          Categories
        </h2>
        <Button
          className="bg-[var(--accent-primary)] hover:bg-[var(--accent-hover)] text-white"
          onClick={() => {
            setEditingCategory(null);
            setFormOpen(true);
          }}
        >
          <Plus className="h-4 w-4" />
          New Category
        </Button>
      </div>

      {/* Table */}
      <div className="rounded-lg border border-[var(--border-primary)] overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b border-[var(--border-primary)] bg-[var(--bg-secondary)]">
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)]">
                  Color
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)]">
                  Name
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)] hidden md:table-cell">
                  Slug
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)] hidden lg:table-cell">
                  Description
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)]">
                  Articles
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)]">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--border-primary)]">
              {categories.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-sm text-[var(--text-tertiary)]">
                    No categories found.
                  </td>
                </tr>
              ) : (
                categories.map((category) => (
                  <tr
                    key={category.id}
                    className="hover:bg-[var(--bg-hover)] transition-colors"
                  >
                    <td className="px-4 py-3">
                      <div
                        className="h-4 w-4 rounded-full"
                        style={{ backgroundColor: category.color }}
                      />
                    </td>
                    <td className="px-4 py-3">
                      <span className="text-sm font-medium text-[var(--text-primary)]">
                        {category.name}
                      </span>
                    </td>
                    <td className="px-4 py-3 hidden md:table-cell">
                      <span className="text-sm text-[var(--text-secondary)]">
                        {category.slug}
                      </span>
                    </td>
                    <td className="px-4 py-3 hidden lg:table-cell">
                      <span className="text-sm text-[var(--text-secondary)]">
                        {truncate(category.description, 50)}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <span className="text-sm text-[var(--text-secondary)]">
                        {category.article_count || 0}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-right">
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="ghost" size="icon" className="h-8 w-8">
                            <MoreHorizontal className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem
                            onClick={() => {
                              setEditingCategory(category);
                              setFormOpen(true);
                            }}
                          >
                            <Pencil className="h-4 w-4 mr-2" />
                            Edit
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            className="text-red-400 focus:text-red-400"
                            onClick={() => setDeleteId(category.id)}
                          >
                            <Trash2 className="h-4 w-4 mr-2" />
                            Delete
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Create/Edit form dialog */}
      <CategoryForm
        open={formOpen}
        onOpenChange={(open) => {
          setFormOpen(open);
          if (!open) setEditingCategory(null);
        }}
        title={editingCategory ? "Edit Category" : "New Category"}
        initialData={
          editingCategory
            ? {
                name: editingCategory.name,
                slug: editingCategory.slug,
                description: editingCategory.description,
                color: editingCategory.color,
              }
            : undefined
        }
        onSubmit={editingCategory ? handleEdit : handleCreate}
      />

      {/* Delete confirmation */}
      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteId(null);
        }}
        title="Delete Category"
        description="Are you sure you want to delete this category? Articles in this category will need to be reassigned."
        confirmLabel="Delete"
        onConfirm={handleDelete}
        variant="destructive"
      />
    </div>
  );
}