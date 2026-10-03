import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { HeartHandshake } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { getIndividualAccount } from "@/lib/orgApi";
import IndividualSurveyForm, { loadIndividualSurvey } from "@/components/IndividualSurveyForm";

// Individual onboarding survey (Part 2): runs after signup, before ID
// verification. Segmentation only — answers set the default risk tier,
// never live limits.
const IndividualOnboarding = () => {
  const navigate = useNavigate();
  const [done, setDone] = useState(false);

  const accountQuery = useQuery({ queryKey: ["individual", "account"], queryFn: () => getIndividualAccount(), staleTime: 30_000 });
  const surveyQuery = useQuery({
    queryKey: ["individual", "account", "survey"],
    queryFn: () => loadIndividualSurvey(),
    staleTime: 15_000,
  });
  const account = accountQuery.data;

  if (!accountQuery.isLoading && !account) {
    return <p className="mt-4 text-sm text-slate-500">Personal account not found.</p>;
  }

  return (
    <div className="mx-auto max-w-xl">
      <div className="flex items-center gap-2">
        <HeartHandshake className="h-6 w-6 text-slate-700" />
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Tell us about yourself</h2>
          <p className="mt-1 text-sm text-slate-500">
            A few quick questions so we can set safe starting defaults. Next: ID verification.
          </p>
        </div>
      </div>

      {accountQuery.isLoading || surveyQuery.isLoading ? (
        <Skeleton className="mt-4 h-64 w-full" />
      ) : done || surveyQuery.data ? (
        <Card className="mt-4">
          <CardContent className="p-4 text-sm text-slate-600">
            <p className="font-medium text-slate-800">Answers saved ✓</p>
            <p className="mt-1">You can change them anytime under Settings → Survey. Next step is ID verification.</p>
            <div className="mt-4 flex gap-2">
              <Button onClick={() => navigate("/individual/verify")}>Continue to verification</Button>
              <Button variant="outline" onClick={() => navigate("/individual/settings")}>Back to settings</Button>
            </div>
          </CardContent>
        </Card>
      ) : account ? (
        <Card className="mt-4">
          <CardContent className="p-4 sm:p-6">
            <IndividualSurveyForm
              account={account}
              submitLabel="Save and continue"
              onSaved={() => setDone(true)}
            />
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
};

export default IndividualOnboarding;
