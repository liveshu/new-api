/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
// BEGIN: 固定分组预算编辑，组合现有 Combobox、Input、Button 和错误展示。
import { useQuery } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { getGroups } from '@/features/users/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import { groupRetryEntries } from './routing-form'

type GroupRetryEditorProps = {
  value: string
  onChange: (value: string) => void
}

export function GroupRetryEditor(props: GroupRetryEditorProps) {
  const { t } = useTranslation()
  const inputId = useId()
  const [newGroup, setNewGroup] = useState('')
  const groups = useQuery({
    queryKey: ['groups'],
    queryFn: async () => requireServerSuccess(await getGroups()),
  })
  const entries = groupRetryEntries(props.value)
  if (entries === null) {
    return (
      <p role='alert'>
        {t(
          'Fixed group retries must map group names to integers from 0 to 99, excluding auto'
        )}
      </p>
    )
  }
  const groupNames = Array.isArray(groups.data?.data) ? groups.data.data : []
  const options = groupNames.filter(
    (group) => group !== 'auto' && !Object.hasOwn(entries, group)
  )
  const canAdd =
    newGroup.trim() !== '' &&
    newGroup !== 'auto' &&
    !Object.hasOwn(entries, newGroup)

  return (
    <div className='min-w-0 space-y-3'>
      {Object.entries(entries).map(([group, retries], index) => (
        <div key={group} className='flex min-w-0 flex-wrap items-center gap-3'>
          {/* BEGIN: 分组名不撑满整行，次数紧随其后；长名称限制宽度并换行。 */}
          <label
            htmlFor={`${inputId}-${index}`}
            className='max-w-full min-w-0 text-sm break-words'
          >
            {t('Retries for {{group}}', { group })}
          </label>
          <Input
            id={`${inputId}-${index}`}
            type='number'
            min={0}
            max={99}
            step={1}
            className='w-24 shrink-0'
            value={
              typeof retries === 'number' || typeof retries === 'string'
                ? retries
                : ''
            }
            onChange={(event) =>
              props.onChange(
                JSON.stringify({
                  ...entries,
                  [group]:
                    event.target.value === '' ? '' : Number(event.target.value),
                })
              )
            }
          />
          {/* END: 固定分组名称和次数紧凑排列。 */}
          <Button
            type='button'
            variant='outline'
            aria-label={t('Use default for {{group}}', { group })}
            onClick={() =>
              props.onChange(
                JSON.stringify(
                  Object.fromEntries(
                    Object.entries(entries).filter(([name]) => name !== group)
                  )
                )
              )
            }
          >
            {t('Use default')}
          </Button>
        </div>
      ))}
      <div className='flex min-w-0 flex-wrap items-center gap-3'>
        <Combobox
          options={options.map((group) => ({ label: group, value: group }))}
          value={newGroup}
          allowCustomValue
          onValueChange={(value) => setNewGroup(value ?? '')}
          aria-label={t('Fixed group name')}
          placeholder={t('Fixed group name')}
          className='min-w-0 flex-1'
        />
        <Button
          type='button'
          variant='outline'
          disabled={!canAdd}
          onClick={() => {
            props.onChange(JSON.stringify({ ...entries, [newGroup]: 0 }))
            setNewGroup('')
          }}
        >
          {t('Add group override')}
        </Button>
      </div>
      {groups.isError && (
        <ErrorState
          title={t('Failed to load groups')}
          onRetry={() => void groups.refetch()}
        />
      )}
    </div>
  )
}
// END: 固定分组预算编辑。
