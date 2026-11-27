// Changing the instance's name and cities after the setup (maintainers; docs/architecture.md §3.4).

import { type Instance, parseInstance } from '@/instance/instance'
import { postAuth } from '@/lib/authApi'

/** POST /api/osfm/instance: saves the name and the cities, the same rules as the setup. */
export async function updateInstance(instance: Instance): Promise<Instance> {
  return parseInstance(await postAuth('/instance', instance))
}
