"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useParams } from "next/navigation";
import type { Article, Category, Author } from "@technews/shared";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ImageUploader } from "@/components/image-uploader";
import { showToast } from "@/components/toast";
import { articlesApi, categoriesApi } from "@/lib/api";
import { ArrowLeft, Loader2 } from "lucide-react";

export default function EditArticlePage() {
  const router = useRouter();
  const params = useParams();
  const articleId = Number(params.id);

  const [article, setArticle] = useState<(Article & { author: Author; category: Category }) | null>(null);
  const [categories, setCategories] = useState<Category[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  
  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [content, setContent] = useState("");
  const [excerpt, setExcerpt] = useState("");
  const [status, setStatus] = useState<string>("draft");
  const [publishDate, setPublishDate] = useState("");
  const [categoryId, setCategoryId] = useState("");
  const [featuredImage, setFeaturedImage] = useState<string | null>(null);
  const [metaTitle, setMetaTitle] = useState("");
  const [metaDescription, setMetaDescription] = useState("");

  // Fetch article and categories on mount
  useEffect(() => {
    const fetchData = async () => {
      setLoading(true);
      try {
        const [articleResponse, categoriesResponse] = await Promise.all([
          articlesApi.getById(articleId),
          categoriesApi.list(),
        ]);
        
        const articleData = articleResponse.article;
        setArticle(articleData);
        setCategories(categoriesResponse.categories);
        
        // Set form values
        setTitle(articleData.title);
        setSlug(articleData.slug);
        setContent(articleData.content);
        setExcerpt(articleData.excerpt);
        setStatus(articleData.status);
        setPublishDate(articleData.published_at 
          ? new Date(articleData.published_at).toISOString().slice(0, 16)
          : ""
        );
        setCategoryId(articleData.category_id.toString());
        setFeaturedImage(articleData.featured_image);
        setMetaTitle(articleData.meta_title || "");
        setMetaDescription(articleData.meta_description || "");
      } catch (error) {
        console.error('Failed to fetch article:', error);
        showToast("Failed to load article", "error");
      } finally {
        setLoading(false);
      }
    };
    
    fetchData();
  }, [articleId]);

  const validateForm = () => {
    if (!title.trim()) {
      showToast("Title is required", "error");
      return false;
    }
    if (!slug.trim()) {
      showToast("Slug is required", "error");
      return false;
    }
    if (!categoryId) {
      showToast("Category is required", "error");
      return false;
    }
    return true;
  };

  const handleSave = async (newStatus?: "draft" | "published" | "scheduled") => {
    if (!validateForm()) return;
    
    setSaving(true);
    try {
      const updateData = {
        title: title.trim(),
        slug: slug.trim(),
        excerpt: excerpt.trim(),
        content: content.trim(),
        featured_image: featuredImage,
        category_id: parseInt(categoryId),
        status: (newStatus || status) as "draft" | "published" | "scheduled",
        published_at: (newStatus || status) === "published" ? (publishDate || new Date().toISOString()) : 
                     (newStatus || status) === "scheduled" ? publishDate : 
                     status === "published" && publishDate ? publishDate : null,
        meta_title: metaTitle.trim() || null,
        meta_description: metaDescription.trim() || null,
      };
      
      await articlesApi.update(articleId, updateData);
      showToast("Article updated successfully", "success");
      router.push("/articles");
    } catch (error: any) {
      console.error('Failed to update article:', error);
      showToast(error.message || "Failed to update article", "error");
    } finally {
      setSaving(false);
    }
  };

  const handleSaveDraft = () => handleSave("draft");
  const handlePublish = () => handleSave("published");
  const handleSchedule = () => handleSave("scheduled");

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="text-center">
          <Loader2 className="h-8 w-8 animate-spin mx-auto mb-4" />
          <p className="text-sm text-[var(--text-secondary)]">Loading article...</p>
        </div>
      </div>
    );
  }

  if (!article) {
    return (
      <div className="flex flex-col items-center justify-center py-20">
        <p className="text-[var(--text-secondary)] mb-4">Article not found</p>
        <Link href="/articles">
          <Button variant="outline">Back to Articles</Button>
        </Link>
      </div>
    );
  }

  return (
    <div>
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <Link
            href="/articles"
            className="rounded-md p-1.5 text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-colors"
          >
            <ArrowLeft className="h-5 w-5" />
          </Link>
          <h2 className="text-2xl font-bold text-[var(--text-primary)]">
            Edit Article
          </h2>
        </div>
        <div className="flex items-center gap-2">
          <Button 
            variant="outline" 
            onClick={handleSaveDraft}
            disabled={saving}
          >
            {saving && status === "draft" ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Saving...
              </>
            ) : (
              "Save Draft"
            )}
          </Button>
          {status === "scheduled" ? (
            <Button
              className="bg-[var(--accent-primary)] hover:bg-[var(--accent-hover)] text-white"
              onClick={handleSchedule}
              disabled={saving || !publishDate}
            >
              {saving && status === "scheduled" ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Updating...
                </>
              ) : (
                "Update"
              )}
            </Button>
          ) : (
            <Button
              className="bg-[var(--accent-primary)] hover:bg-[var(--accent-hover)] text-white"
              onClick={handlePublish}
              disabled={saving}
            >
              {saving && status === "published" ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Updating...
                </>
              ) : (
                "Update"
              )}
            </Button>
          )}
        </div>
      </div>

      {/* Two-column layout */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Main content */}
        <div className="lg:col-span-2 space-y-5">
          <div className="space-y-2">
            <Input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Article title"
              className="text-xl font-semibold h-12 border-0 bg-transparent px-0 focus-visible:ring-0 placeholder:text-[var(--text-tertiary)]"
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="slug" className="text-[var(--text-tertiary)]">
              Slug
            </Label>
            <div className="flex items-center">
              <span className="text-sm text-[var(--text-tertiary)] mr-1">
                /article/
              </span>
              <Input
                id="slug"
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
                placeholder="article-slug"
                className="flex-1"
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="content">Content (Markdown)</Label>
            <Textarea
              id="content"
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder="Write your article content in Markdown..."
              className="min-h-[400px] font-mono text-sm"
            />
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label htmlFor="excerpt">Excerpt</Label>
              <span
                className={`text-xs ${
                  excerpt.length > 280
                    ? "text-[var(--error)]"
                    : "text-[var(--text-tertiary)]"
                }`}
              >
                {excerpt.length}/280
              </span>
            </div>
            <Textarea
              id="excerpt"
              value={excerpt}
              onChange={(e) => setExcerpt(e.target.value.slice(0, 280))}
              placeholder="Brief summary of the article..."
              rows={3}
            />
          </div>
        </div>

        {/* Sidebar */}
        <div className="space-y-5">
          <div className="rounded-lg border border-[var(--border-primary)] bg-[var(--bg-secondary)] p-4 space-y-4">
            <h3 className="text-sm font-semibold text-[var(--text-primary)] uppercase tracking-wider">
              Publishing
            </h3>

            <div className="space-y-2">
              <Label>Status</Label>
              <Select value={status} onValueChange={setStatus}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="draft">Draft</SelectItem>
                  <SelectItem value="published">Published</SelectItem>
                  <SelectItem value="scheduled">Scheduled</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="publish-date">Publish Date</Label>
              <Input
                id="publish-date"
                type="datetime-local"
                value={publishDate}
                onChange={(e) => setPublishDate(e.target.value)}
              />
            </div>

            <div className="space-y-2">
              <Label>Category</Label>
              <Select value={categoryId} onValueChange={setCategoryId}>
                <SelectTrigger>
                  <SelectValue placeholder="Select category" />
                </SelectTrigger>
                <SelectContent>
                  {categories.map((cat) => (
                    <SelectItem key={cat.id} value={cat.id.toString()}>
                      {cat.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="rounded-lg border border-[var(--border-primary)] bg-[var(--bg-secondary)] p-4 space-y-4">
            <h3 className="text-sm font-semibold text-[var(--text-primary)] uppercase tracking-wider">
              Featured Image
            </h3>
            <ImageUploader
              value={featuredImage}
              onChange={setFeaturedImage}
            />
          </div>

          <div className="rounded-lg border border-[var(--border-primary)] bg-[var(--bg-secondary)] p-4 space-y-4">
            <h3 className="text-sm font-semibold text-[var(--text-primary)] uppercase tracking-wider">
              SEO
            </h3>

            <div className="space-y-2">
              <Label htmlFor="meta-title">Meta Title</Label>
              <Input
                id="meta-title"
                value={metaTitle}
                onChange={(e) => setMetaTitle(e.target.value)}
                placeholder="SEO title"
              />
            </div>

            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label htmlFor="meta-desc">Meta Description</Label>
                <span
                  className={`text-xs ${
                    metaDescription.length > 160
                      ? "text-[var(--error)]"
                      : "text-[var(--text-tertiary)]"
                  }`}
                >
                  {metaDescription.length}/160
                </span>
              </div>
              <Textarea
                id="meta-desc"
                value={metaDescription}
                onChange={(e) =>
                  setMetaDescription(e.target.value.slice(0, 160))
                }
                placeholder="SEO description"
                rows={3}
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}