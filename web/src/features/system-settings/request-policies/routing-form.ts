import type { TFunction } from 'i18next'
import { z } from 'zod'

import { parseHttpStatusCodeRules } from '@/lib/http-status-code-rules'

// BEGIN: 固定分组映射的对象结构解析复用于表单校验和可视化编辑。
export function groupRetryEntries(raw: string): Record<string, unknown> | null {
  try {
    const value: unknown = JSON.parse(raw)
    if (value === null || typeof value !== 'object' || Array.isArray(value)) {
      return null
    }
    return value as Record<string, unknown>
  } catch {
    return null
  }
}

export function validGroupRetries(raw: string): boolean {
  const entries = groupRetryEntries(raw)
  return (
    entries !== null &&
    Object.entries(entries).every(
      ([group, retries]) =>
        group.trim() !== '' &&
        group !== 'auto' &&
        typeof retries === 'number' &&
        Number.isInteger(retries) &&
        retries >= 0 &&
        retries <= 99
    )
  )
}

export function normalizeGroupRetries(raw: string): string {
  const entries = groupRetryEntries(raw)
  if (!entries) return raw
  return JSON.stringify(
    Object.fromEntries(
      Object.entries(entries).sort(([a], [b]) => a.localeCompare(b))
    )
  )
}
// END: 固定分组映射解析。

export function createRoutingPolicySchema(t: TFunction) {
  return z.object({
    RetryTimes: z.number().int().min(0).max(99),
    // BEGIN: 显式 0 有效，Auto 不允许配置覆盖。
    GroupRetryTimes: z
      .string()
      .refine(
        validGroupRetries,
        t(
          'Fixed group retries must map group names to integers from 0 to 99, excluding auto'
        )
      ),
    // END: 固定分组重试校验。
    AutomaticRetryStatusCodes: z
      .string()
      .refine(
        (value) => parseHttpStatusCodeRules(value).ok,
        t('Invalid status code rules')
      ),
    channel_affinity_setting: z.object({
      enabled: z.boolean(),
      session_mode: z.enum(['', 'off', 'prefer', 'strict']),
      switch_on_success: z.boolean(),
      keep_on_channel_disabled: z.boolean(),
      max_entries: z.number().int().min(0),
      default_ttl_seconds: z.number().int().min(0),
      rules: z.string().superRefine((value, context) => {
        try {
          const rules: unknown = JSON.parse(value)
          if (!Array.isArray(rules)) {
            context.addIssue({
              code: 'custom',
              message: t('Rules JSON must be an array'),
            })
          }
        } catch {
          context.addIssue({
            code: 'custom',
            message: t('Invalid rules JSON format'),
          })
        }
      }),
    }),
  })
}

export type RoutingPolicyFormValues = z.infer<
  ReturnType<typeof createRoutingPolicySchema>
>

export function routingPolicyFormValues(
  options: Record<string, string>
): RoutingPolicyFormValues {
  return {
    RetryTimes: Number(options.RetryTimes),
    // BEGIN: 老版本缺少覆盖字段时默认继承。
    GroupRetryTimes: options.GroupRetryTimes ?? '{}',
    // END: 固定分组重试回填。
    AutomaticRetryStatusCodes: options.AutomaticRetryStatusCodes,
    channel_affinity_setting: {
      enabled: options['channel_affinity_setting.enabled'] === 'true',
      session_mode: (options['channel_affinity_setting.session_mode'] ||
        '') as RoutingPolicyFormValues['channel_affinity_setting']['session_mode'],
      switch_on_success:
        options['channel_affinity_setting.switch_on_success'] === 'true',
      keep_on_channel_disabled:
        options['channel_affinity_setting.keep_on_channel_disabled'] === 'true',
      max_entries: Number(options['channel_affinity_setting.max_entries']),
      default_ttl_seconds: Number(
        options['channel_affinity_setting.default_ttl_seconds']
      ),
      rules: options['channel_affinity_setting.rules'] || '[]',
    },
  }
}

export function routingPolicyOptions(
  values: RoutingPolicyFormValues
): Record<string, string> {
  return {
    RetryTimes: String(values.RetryTimes),
    // BEGIN: 稳定序列化，不丢失显式 0。
    GroupRetryTimes: normalizeGroupRetries(values.GroupRetryTimes),
    // END: 固定分组重试保存。
    AutomaticRetryStatusCodes: values.AutomaticRetryStatusCodes,
    ...Object.fromEntries(
      Object.entries(values.channel_affinity_setting).map(([key, value]) => [
        `channel_affinity_setting.${key}`,
        String(value),
      ])
    ),
  }
}
