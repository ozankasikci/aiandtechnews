"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import type { Article, Category, Author } from "@technews/shared";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { StatusBadge } from "@/components/status-badge";
import { CategoryTag } from "@/components/category-tag";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { showToast } from "@/components/toast";
import { articlesApi, categoriesApi } from "@/lib/api";
import { formatDate } from "@/lib/utils";
import {
  Plus,
  Search,
  MoreHorizontal,
  Pencil,
  Trash2,
  ChevronLeft,
  ChevronRight,
  Loader2,
} from "lucide-react";

const ITEMS_PER_PAGE = 20;

export default function ArticlesPage() {
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [categoryFilter, setCategoryFilter] = useState("all");
  const [page, setPage] = useState(1);
  const [deleteId, setDeleteId] = useState<number | null>(null);
  
  // API state
  const [articles, setArticles] = useState<(Article & { author: Author; category: Category })[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [loading, setLoading] = useState(true);
  const [totalPages, setTotalPages] = useState(1);
  const [total, setTotal] = useState(0);

  // Fetch categories on mount
  useEffect(() => {
    const fetchCategories = async () => {
      try {
        const { categories } = await categoriesApi.list();
        setCategories(categories);
      } catch (error) {
        console.error('Failed to fetch categories:', error);
        showToast("Failed to load categories", "error");
      }
    };
    
    fetchCategories();
  }, []);

  // Fetch articles when filters change
  useEffect(() => {
    const fetchArticles = async () => {
      setLoading(true);
      try {
        const params: any = {
          page,
          limit: ITEMS_PER_PAGE,
        };
        
        if (statusFilter !== "all") {
          params.status = statusFilter;
        }
        
        if (categoryFilter !== "all") {
          const category = categories.find(c => c.id.toString() === categoryFilter);
          if (category) {
            params.category = category.slug;
          }
        }
        
        if (search) {
          params.search = search;
        }
        
        const response = await articlesApi.list(params);
        setArticles(response.articles);
        setTotal(response.total);
        setTotalPages(response.totalPages);
      } catch (error) {
        console.error('Failed to fetch articles:', error);
        showToast("Failed to load articles", "error");
      } finally {
        setLoading(false);
      }
    };
    
    // Only fetch if categories are loaded (to avoid unnecessary requests)
    if (categories.length > 0 || categoryFilter === "all") {
      fetchArticles();
    }
  }, [search, statusFilter, categoryFilter, page, categories]);

  const handleDelete = async () => {
    if (deleteId !== null) {
      try {
        await articlesApi.delete(deleteId);
        setDeleteId(null);
        showToast("Article deleted successfully", "success");
        
        // Refresh the current page
        const params: any = {
          page,
          limit: ITEMS_PER_PAGE,
        };
        
        if (statusFilter !== "all") params.status = statusFilter;
        if (categoryFilter !== "all") {
          const category = categories.find(c => c.id.toString() === categoryFilter);
          if (category) params.category = category.slug;
        }
        if (search) params.search = search;
        
        const response = await articlesApi.list(params);
        setArticles(response.articles);
        setTotal(response.total);
        setTotalPages(response.totalPages);
      } catch (error: any) {
        console.error('Failed to delete article:', error);
        showToast(error.message || "Failed to delete article", "error");
        setDeleteId(null);
      }
    }
  };

  return (
    <div>
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-bold text-[var(--text-primary)]">
          Articles
        </h2>
        <Link href="/articles/new">
          <Button className="bg-[var(--accent-primary)] hover:bg-[var(--accent-hover)] text-white">
            <Plus className="h-4 w-4" />
            New Article
          </Button>
        </Link>
      </div>

      {/* Filters */}
      <div className="flex flex-col sm:flex-row gap-3 mb-6">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-[var(--text-tertiary)]" />
          <Input
            placeholder="Search articles..."
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setPage(1);
            }}
            className="pl-9"
          />
        </div>
        <Select
          value={statusFilter}
          onValueChange={(v) => {
            setStatusFilter(v);
            setPage(1);
          }}
        >
          <SelectTrigger className="w-full sm:w-[160px]">
            <SelectValue placeholder="Status" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Statuses</SelectItem>
            <SelectItem value="draft">Draft</SelectItem>
            <SelectItem value="published">Published</SelectItem>
            <SelectItem value="scheduled">Scheduled</SelectItem>
          </SelectContent>
        </Select>
        <Select
          value={categoryFilter}
          onValueChange={(v) => {
            setCategoryFilter(v);
            setPage(1);
          }}
        >
          <SelectTrigger className="w-full sm:w-[200px]">
            <SelectValue placeholder="Category" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Categories</SelectItem>
            {categories.map((cat) => (
              <SelectItem key={cat.id} value={cat.id.toString()}>
                {cat.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Table */}
      <div className="rounded-lg border border-[var(--border-primary)] overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b border-[var(--border-primary)] bg-[var(--bg-secondary)]">
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)]">
                  Title
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)]">
                  Status
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)] hidden md:table-cell">
                  Category
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)] hidden lg:table-cell">
                  Author
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)] hidden sm:table-cell">
                  Date
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-[var(--text-tertiary)]">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--border-primary)]">
              {loading ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center">
                    <div className="flex items-center justify-center gap-2">
                      <Loader2 className="h-4 w-4 animate-spin" />
                      <span className="text-sm text-[var(--text-secondary)]">Loading articles...</span>
                    </div>
                  </td>
                </tr>
              ) : articles.length === 0 ? (
                <tr>
                  <td
                    colSpan={6}
                    className="px-4 py-12 text-center text-sm text-[var(--text-tertiary)]"
                  >
                    No articles found.
                  </td>
                </tr>
              ) : (
                articles.map((article) => (
                  <tr
                    key={article.id}
                    className="hover:bg-[var(--bg-hover)] transition-colors"
                  >
                    <td className="px-4 py-3">
                      <div>
                        <p className="text-sm font-medium text-[var(--text-primary)] line-clamp-1">
                          {article.title}
                        </p>
                        <p className="text-xs text-[var(--text-tertiary)] mt-0.5">
                          /article/{article.slug}
                        </p>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <StatusBadge status={article.status} />
                    </td>
                    <td className="px-4 py-3 hidden md:table-cell">
                      <CategoryTag
                        name={article.category.name}
                        color={article.category.color}
                      />
                    </td>
                    <td className="px-4 py-3 hidden lg:table-cell">
                      <span className="text-sm text-[var(--text-secondary)]">
                        {article.author.name}
                      </span>
                    </td>
                    <td className="px-4 py-3 hidden sm:table-cell">
                      <span className="text-sm text-[var(--text-secondary)]">
                        {formatDate(article.published_at || article.created_at)}
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
                          <DropdownMenuItem asChild>
                            <Link href={`/articles/${article.id}/edit`}>
                              <Pencil className="h-4 w-4 mr-2" />
                              Edit
                            </Link>
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            className="text-red-400 focus:text-red-400"
                            onClick={() => setDeleteId(article.id)}
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

      {/* Pagination */}
      {!loading && totalPages > 1 && (
        <div className="flex items-center justify-between mt-4">
          <p className="text-sm text-[var(--text-tertiary)]">
            Showing {(page - 1) * ITEMS_PER_PAGE + 1} to{" "}
            {Math.min(page * ITEMS_PER_PAGE, total)} of{" "}
            {total} articles
          </p>
          <div className="flex items-center gap-1">
            <Button
              variant="outline"
              size="icon"
              className="h-8 w-8"
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page === 1}
            >
              <ChevronLeft className="h-4 w-4" />
            </Button>
            {Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
              let pageNum = i + 1;
              if (totalPages > 5) {
                if (page <= 3) {
                  pageNum = i + 1;
                } else if (page > totalPages - 3) {
                  pageNum = totalPages - 4 + i;
                } else {
                  pageNum = page - 2 + i;
                }
              }
              return (
                <Button
                  key={pageNum}
                  variant={pageNum === page ? "default" : "outline"}
                  size="icon"
                  className="h-8 w-8"
                  onClick={() => setPage(pageNum)}
                >
                  {pageNum}
                </Button>
              );
            })}
            <Button
              variant="outline"
              size="icon"
              className="h-8 w-8"
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page === totalPages}
            >
              <ChevronRight className="h-4 w-4" />
            </Button>
          </div>
        </div>
      )}

      {/* Delete confirmation */}
      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteId(null);
        }}
        title="Delete Article"
        description="Are you sure you want to delete this article? This action cannot be undone."
        confirmLabel="Delete"
        onConfirm={handleDelete}
        variant="destructive"
      />
    </div>
  );
}
