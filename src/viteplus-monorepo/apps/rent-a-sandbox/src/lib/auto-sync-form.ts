import { useCallback, useReducer, useRef } from "react";

export type AutoSyncFormStatus = "idle" | "syncing" | "failed";

type AutoSyncFormState<TForm> = {
  readonly error: unknown | null;
  readonly form: TForm;
  readonly status: AutoSyncFormStatus;
};

type AutoSyncFormAction<TForm> =
  | { readonly type: "change"; readonly form: TForm }
  | { readonly type: "failed"; readonly error: unknown }
  | { readonly type: "synced"; readonly form: TForm }
  | { readonly type: "syncing" };

export function useAutoSyncForm<TForm, TRequest, TResult>({
  formEqual,
  formFromResult,
  initialForm,
  initialVersion,
  mutate,
  requestFromForm,
  validate,
  versionFromResult,
}: {
  readonly formEqual: (left: TForm, right: TForm) => boolean;
  readonly formFromResult: (result: TResult) => TForm;
  readonly initialForm: TForm;
  readonly initialVersion: number;
  readonly mutate: (request: TRequest) => Promise<TResult>;
  readonly requestFromForm: (form: TForm, version: number) => TRequest;
  readonly validate?: (form: TForm) => string | null;
  readonly versionFromResult: (result: TResult) => number;
}) {
  const [state, dispatch] = useReducer(autoSyncFormReducer<TForm>, {
    error: null,
    form: initialForm,
    status: "idle",
  });
  const baseFormRef = useRef(initialForm);
  const currentFormRef = useRef(initialForm);
  const versionRef = useRef(initialVersion);
  const inFlightRef = useRef(false);
  const queuedFormRef = useRef<TForm | null>(null);

  const sync = useCallback(
    (candidate: TForm = currentFormRef.current) => {
      const validationError = validate?.(candidate) ?? null;
      if (validationError) {
        queuedFormRef.current = null;
        dispatch({ type: "failed", error: new Error(validationError) });
        return;
      }

      if (formEqual(candidate, baseFormRef.current)) {
        if (!inFlightRef.current) {
          currentFormRef.current = candidate;
          dispatch({ type: "synced", form: candidate });
        }
        return;
      }

      if (inFlightRef.current) {
        queuedFormRef.current = candidate;
        dispatch({ type: "syncing" });
        return;
      }

      inFlightRef.current = true;
      dispatch({ type: "syncing" });
      void mutate(requestFromForm(candidate, versionRef.current))
        .then((result) => {
          const syncedForm = formFromResult(result);
          versionRef.current = versionFromResult(result);
          baseFormRef.current = syncedForm;

          const queuedForm = queuedFormRef.current;
          queuedFormRef.current = null;
          inFlightRef.current = false;
          if (queuedForm && !formEqual(queuedForm, syncedForm)) {
            sync(queuedForm);
            return;
          }

          currentFormRef.current = syncedForm;
          dispatch({ type: "synced", form: syncedForm });
        })
        .catch((error: unknown) => {
          queuedFormRef.current = null;
          inFlightRef.current = false;
          dispatch({ type: "failed", error });
        });
    },
    [formEqual, formFromResult, mutate, requestFromForm, validate, versionFromResult],
  );

  const change = useCallback((update: (current: TForm) => TForm) => {
    const form = update(currentFormRef.current);
    currentFormRef.current = form;
    if (inFlightRef.current) {
      queuedFormRef.current = form;
    }
    dispatch({ type: "change", form });
    return form;
  }, []);

  return {
    change,
    error: state.error,
    form: state.form,
    status: state.status,
    sync,
  };
}

function autoSyncFormReducer<TForm>(
  state: AutoSyncFormState<TForm>,
  action: AutoSyncFormAction<TForm>,
): AutoSyncFormState<TForm> {
  switch (action.type) {
    case "change":
      return { error: null, form: action.form, status: state.status };
    case "failed":
      return { ...state, error: action.error, status: "failed" };
    case "synced":
      return { error: null, form: action.form, status: "idle" };
    case "syncing":
      return { ...state, error: null, status: "syncing" };
  }
}
