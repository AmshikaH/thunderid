// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

import {cn} from '@thunderid/utils';
import {Box, FormControl, FormLabel, TreeView, Typography} from '@wso2/oxygen-ui';
import type {JSX, SyntheticEvent} from 'react';
import {useMemo} from 'react';
import {useTranslation} from 'react-i18next';
import SelectAdapter from './SelectAdapter';
import type {FlowFieldProps, OrganizationUnitTreeNode} from '../../../models/flow';

interface OUTreeItem {
  id: string;
  label: string;
  handle: string;
  children?: OUTreeItem[];
}

function toTreeItems(nodes: OrganizationUnitTreeNode[]): OUTreeItem[] {
  return nodes.map((node) => ({
    id: node.id,
    label: node.name,
    handle: node.handle,
    children: node.children && node.children.length > 0 ? toTreeItems(node.children) : undefined,
  }));
}

interface OUTreeItemProps extends TreeView.TreeItemProps {
  itemId: string;
  itemMap?: Map<string, OUTreeItem>;
}

function OUTreeItem({itemId, itemMap = undefined, ...restProps}: OUTreeItemProps): JSX.Element {
  const item = itemMap?.get(itemId);
  return (
    <TreeView.TreeItem
      {...restProps}
      itemId={itemId}
      label={
        <Box sx={{py: 0.25}}>
          <Typography variant="body2" sx={{fontWeight: 500}}>
            {item?.label}
          </Typography>
          {item?.handle && (
            <Typography variant="caption" color="text.secondary">
              {item.handle}
            </Typography>
          )}
        </Box>
      }
    />
  );
}

function buildItemMap(
  items: OUTreeItem[],
  map: Map<string, OUTreeItem> = new Map<string, OUTreeItem>(),
): Map<string, OUTreeItem> {
  for (const item of items) {
    map.set(item.id, item);
    if (item.children) buildItemMap(item.children, map);
  }
  return map;
}

/**
 * Renders an `OU_SELECT` input.
 *
 * When the backend forwards `component.tree` (the "promptAll" strategy's full organization unit
 * hierarchy), this renders a real expand/collapse tree and submits the clicked node's ID directly
 * — ids are globally unique, so no handle-based resolution or disambiguation is needed.
 *
 * Otherwise `component.options` is a flat list of candidate organization unit handles (never
 * IDs) — resolved to the underlying organization unit ID server-side, the same way any other
 * handle-based OU selection is — which is exactly the shape a generic `SELECT` input already
 * renders, so that case delegates straight to `SelectAdapter` instead of duplicating it.
 */
export default function OUSelectAdapter(props: FlowFieldProps): JSX.Element | null {
  const {component, values, touched, fieldErrors, isLoading, resolve, onInputChange} = props;
  const {t} = useTranslation();
  const {ref, options, tree, hint} = component;

  const treeItems = useMemo(() => (tree && tree.length > 0 ? toTreeItems(tree) : undefined), [tree]);
  const treeItemMap = useMemo(() => (treeItems ? buildItemMap(treeItems) : undefined), [treeItems]);

  if (!ref || typeof ref !== 'string' || (!options && !treeItems)) return null;

  if (!treeItems || !treeItemMap) {
    return (
      <SelectAdapter
        {...props}
        component={{...component, placeholder: component.placeholder ?? 'Select an organization'}}
      />
    );
  }

  const hasError = !!(touched?.[ref] && fieldErrors?.[ref]);
  const value = values[ref] ?? '';

  return (
    <FormControl fullWidth className={cn('Flow--ou-select', 'FormControl--root')}>
      <FormLabel htmlFor={ref} className={cn('Label--root')}>
        {t(resolve(component.label)!)}
      </FormLabel>
      <Box
        className={cn('Flow--ou-tree-scroll')}
        sx={{maxHeight: 280, overflow: 'auto', border: '1px solid', borderColor: 'divider', borderRadius: 1, p: 0.5}}
      >
        <TreeView.RichTreeView
          id={ref}
          items={treeItems}
          selectedItems={value || null}
          onSelectedItemsChange={(_event: SyntheticEvent | null, itemId: string | null) => {
            if (itemId && !isLoading) onInputChange(ref, itemId);
          }}
          getItemLabel={(item: OUTreeItem) => item.label}
          slots={{item: OUTreeItem}}
          slotProps={{item: {itemMap: treeItemMap} as Record<string, unknown>}}
        />
      </Box>
      {hasError && (
        <Typography variant="caption" color="error.main" sx={{mt: 0.5}}>
          {fieldErrors?.[ref]}
        </Typography>
      )}
      {hint && (
        <Typography variant="caption" color="text.secondary">
          {hint}
        </Typography>
      )}
    </FormControl>
  );
}
