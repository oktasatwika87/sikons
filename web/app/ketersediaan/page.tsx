"use client";

import { useState } from "react";
import { RequireAuth } from "@/components/auth/RequireAuth";
import { RuleList } from "@/components/availability/RuleList";
import { RuleForm } from "@/components/availability/RuleForm";
import { ExceptionList } from "@/components/availability/ExceptionList";
import { ExceptionForm } from "@/components/availability/ExceptionForm";
import { ReconcileSummary } from "@/components/availability/ReconcileSummary";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { SlotReconcileSummary } from "@/lib/api/types";

/** Rentang pengecualian: 90 hari ke depan. */
function getExceptionRange(): { from: string; to: string } {
  const now = new Date();
  const to = new Date(now.getTime() + 90 * 24 * 60 * 60 * 1000);
  return {
    from: now.toISOString().slice(0, 10),
    to: to.toISOString().slice(0, 10),
  };
}

function KetersediaanContent() {
  const [summary, setSummary] = useState<SlotReconcileSummary | null>(null);
  const [showRuleForm, setShowRuleForm] = useState(false);
  const [showExceptionForm, setShowExceptionForm] = useState(false);

  const range = getExceptionRange();

  function handleSummary(summary: SlotReconcileSummary) {
    setSummary(summary);
    setShowRuleForm(false);
    setShowExceptionForm(false);
  }

  return (
    <div className="container mx-auto max-w-4xl px-4 py-8">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Ketersediaan Jadwal</h1>
      </div>

      {/* Summary panel */}
      {summary && (
        <div className="mb-6">
          <ReconcileSummary summary={summary} onDismiss={() => setSummary(null)} />
        </div>
      )}

      {/* Jadwal rutin */}
      <section className="mb-8">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-medium">Jadwal Rutin</h2>
          <Button
            onClick={() => setShowRuleForm(true)}
            type="button"
            size="sm"
          >
            Tambah Aturan
          </Button>
        </div>

        <Card>
          <CardContent className="pt-4">
            {showRuleForm ? (
              <div className="mb-4 rounded-lg border border-dashed border-border p-4">
                <h3 className="mb-4 text-sm font-medium">Aturan Baru</h3>
                <RuleForm
                  onSuccess={handleSummary}
                  onCancel={() => setShowRuleForm(false)}
                />
              </div>
            ) : (
              <RuleList onSummary={handleSummary} />
            )}
          </CardContent>
        </Card>
      </section>

      {/* Pengecualian */}
      <section>
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-medium">Pengecualian</h2>
          <Button
            onClick={() => setShowExceptionForm(true)}
            type="button"
            size="sm"
          >
            Tambah Pengecualian
          </Button>
        </div>

        <Card>
          <CardContent className="pt-4">
            {showExceptionForm ? (
              <div className="mb-4 rounded-lg border border-dashed border-border p-4">
                <h3 className="mb-4 text-sm font-medium">Pengecualian Baru</h3>
                <ExceptionForm
                  onSuccess={handleSummary}
                  onCancel={() => setShowExceptionForm(false)}
                />
              </div>
            ) : (
              <ExceptionList
                from={range.from}
                to={range.to}
                onSummary={handleSummary}
              />
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}

export default function KetersediaanPage() {
  return (
    <RequireAuth>
      <KetersediaanContent />
    </RequireAuth>
  );
}
