"use client";

import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { showToast } from "@/components/toast";
import { settingsApi } from "@/lib/api";
import { Loader2 } from "lucide-react";
import type { Settings } from "@technews/shared";

export default function SettingsPage() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const [siteName, setSiteName] = useState("");
  const [siteDescription, setSiteDescription] = useState("");
  const [socialTwitter, setSocialTwitter] = useState("");
  const [socialLinkedin, setSocialLinkedin] = useState("");
  const [socialGithub, setSocialGithub] = useState("");
  const [newsletterEnabled, setNewsletterEnabled] = useState(false);
  const [newsletterProvider, setNewsletterProvider] = useState<string>("none");
  const [newsletterWebhookUrl, setNewsletterWebhookUrl] = useState("");

  // Fetch settings on mount
  useEffect(() => {
    const fetchSettings = async () => {
      setLoading(true);
      try {
        const { settings } = await settingsApi.get();
        setSettings(settings);
        
        // Update form values
        setSiteName(settings.site_name);
        setSiteDescription(settings.site_description);
        setSocialTwitter(settings.social_twitter || "");
        setSocialLinkedin(settings.social_linkedin || "");
        setSocialGithub(settings.social_github || "");
        setNewsletterEnabled(settings.newsletter_enabled);
        setNewsletterProvider(settings.newsletter_provider);
        setNewsletterWebhookUrl(settings.newsletter_webhook_url || "");
      } catch (error) {
        console.error('Failed to fetch settings:', error);
        showToast("Failed to load settings", "error");
      } finally {
        setLoading(false);
      }
    };
    
    fetchSettings();
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      const updateData = {
        site_name: siteName.trim(),
        site_description: siteDescription.trim(),
        social_twitter: socialTwitter.trim() || null,
        social_linkedin: socialLinkedin.trim() || null,
        social_github: socialGithub.trim() || null,
        newsletter_enabled: newsletterEnabled,
        newsletter_provider: newsletterProvider as Settings['newsletter_provider'],
        newsletter_webhook_url: newsletterWebhookUrl.trim() || null,
      };
      
      await settingsApi.update(updateData);
      showToast("Settings saved successfully", "success");
    } catch (error: any) {
      console.error('Failed to save settings:', error);
      showToast(error.message || "Failed to save settings", "error");
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="text-center">
          <Loader2 className="h-8 w-8 animate-spin mx-auto mb-4" />
          <p className="text-sm text-[var(--text-secondary)]">Loading settings...</p>
        </div>
      </div>
    );
  }

  return (
    <div>
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-bold text-[var(--text-primary)]">
          Settings
        </h2>
        <Button
          className="bg-[var(--accent-primary)] hover:bg-[var(--accent-hover)] text-white"
          onClick={handleSave}
          disabled={saving}
        >
          {saving ? (
            <>
              <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              Saving...
            </>
          ) : (
            "Save Changes"
          )}
        </Button>
      </div>

      <div className="max-w-2xl space-y-8">
        {/* Site Information */}
        <div className="space-y-6">
          <div>
            <h3 className="text-lg font-semibold text-[var(--text-primary)] mb-4">
              Site Information
            </h3>
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="site-name">Site Name</Label>
                <Input
                  id="site-name"
                  value={siteName}
                  onChange={(e) => setSiteName(e.target.value)}
                  placeholder="TechNews"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="site-description">Site Description</Label>
                <Textarea
                  id="site-description"
                  value={siteDescription}
                  onChange={(e) => setSiteDescription(e.target.value)}
                  placeholder="Brief description of your site..."
                  rows={3}
                />
              </div>
            </div>
          </div>

          <Separator />

          {/* Social Links */}
          <div>
            <h3 className="text-lg font-semibold text-[var(--text-primary)] mb-4">
              Social Links
            </h3>
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="social-twitter">Twitter/X URL</Label>
                <Input
                  id="social-twitter"
                  value={socialTwitter}
                  onChange={(e) => setSocialTwitter(e.target.value)}
                  placeholder="https://x.com/yourusername"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="social-linkedin">LinkedIn URL</Label>
                <Input
                  id="social-linkedin"
                  value={socialLinkedin}
                  onChange={(e) => setSocialLinkedin(e.target.value)}
                  placeholder="https://linkedin.com/company/yourcompany"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="social-github">GitHub URL</Label>
                <Input
                  id="social-github"
                  value={socialGithub}
                  onChange={(e) => setSocialGithub(e.target.value)}
                  placeholder="https://github.com/yourusername"
                />
              </div>
            </div>
          </div>

          <Separator />

          {/* Newsletter */}
          <div>
            <h3 className="text-lg font-semibold text-[var(--text-primary)] mb-4">
              Newsletter
            </h3>
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label htmlFor="newsletter-enabled">Enable Newsletter</Label>
                  <p className="text-sm text-[var(--text-tertiary)]">
                    Show newsletter signup forms on your site
                  </p>
                </div>
                <Switch
                  id="newsletter-enabled"
                  checked={newsletterEnabled}
                  onCheckedChange={setNewsletterEnabled}
                />
              </div>
              
              {newsletterEnabled && (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="newsletter-provider">Provider</Label>
                    <Select
                      value={newsletterProvider}
                      onValueChange={setNewsletterProvider}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="Select provider" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="none">None</SelectItem>
                        <SelectItem value="mailchimp">Mailchimp</SelectItem>
                        <SelectItem value="convertkit">ConvertKit</SelectItem>
                        <SelectItem value="custom">Custom Webhook</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  
                  {newsletterProvider !== "none" && (
                    <div className="space-y-2">
                      <Label htmlFor="newsletter-webhook">Webhook URL</Label>
                      <Input
                        id="newsletter-webhook"
                        value={newsletterWebhookUrl}
                        onChange={(e) => setNewsletterWebhookUrl(e.target.value)}
                        placeholder="https://api.provider.com/webhook/..."
                      />
                    </div>
                  )}
                </>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}