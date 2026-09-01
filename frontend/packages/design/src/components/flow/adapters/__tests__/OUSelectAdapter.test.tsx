// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

/* eslint-disable @typescript-eslint/no-unsafe-assignment */
import {within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {describe, it, expect, vi} from 'vitest';
import type {FlowFieldProps} from '../../../../models/flow';
import renderWithProviders from '../../../../test/renderWithProviders';
import OUSelectAdapter from '../OUSelectAdapter';

const baseProps: FlowFieldProps = {
  component: {
    id: 'ou_select',
    type: 'OU_SELECT',
    ref: 'ouId',
    label: 'Organization',
    required: true,
    options: ['acme-corp', 'beta-inc'],
  },
  values: {},
  isLoading: false,
  resolve: (s) => s,
  onInputChange: vi.fn(),
};

describe('OUSelectAdapter', () => {
  it('renders the label', () => {
    const {container} = renderWithProviders(<OUSelectAdapter {...baseProps} />);
    expect(within(container).getByText('Organization')).toBeTruthy();
  });

  it('renders each option as a handle', async () => {
    const {container} = renderWithProviders(<OUSelectAdapter {...baseProps} />);
    await userEvent.click(within(container).getByRole('combobox'));

    expect(within(document.body).getByRole('option', {name: 'acme-corp'})).toBeTruthy();
    expect(within(document.body).getByRole('option', {name: 'beta-inc'})).toBeTruthy();
  });

  it('submits the selected handle, not a resolved ID', async () => {
    const onInputChange = vi.fn();
    const {container} = renderWithProviders(<OUSelectAdapter {...baseProps} onInputChange={onInputChange} />);

    await userEvent.click(within(container).getByRole('combobox'));
    await userEvent.click(within(document.body).getByRole('option', {name: 'beta-inc'}));

    expect(onInputChange).toHaveBeenCalledWith('ouId', 'beta-inc');
  });

  it('shows the default placeholder when none is provided', () => {
    const {container} = renderWithProviders(<OUSelectAdapter {...baseProps} />);
    expect(within(container).getByText('Select an organization')).toBeTruthy();
  });

  it('shows a custom placeholder when provided', () => {
    const {container} = renderWithProviders(
      <OUSelectAdapter {...baseProps} component={{...baseProps.component, placeholder: 'Choose your org'}} />,
    );
    expect(within(container).getByText('Choose your org')).toBeTruthy();
  });

  it('returns null when ref is missing', () => {
    const {container} = renderWithProviders(
      <OUSelectAdapter {...baseProps} component={{...baseProps.component, ref: undefined}} />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('returns null when options is missing', () => {
    const {container} = renderWithProviders(
      <OUSelectAdapter {...baseProps} component={{...baseProps.component, options: undefined}} />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('shows the field error when touched and invalid', () => {
    const {container} = renderWithProviders(
      <OUSelectAdapter {...baseProps} touched={{ouId: true}} fieldErrors={{ouId: 'Organization not found'}} />,
    );
    expect(within(container).getByText('Organization not found')).toBeTruthy();
  });

  describe('when component.tree is provided', () => {
    const treeProps: FlowFieldProps = {
      ...baseProps,
      component: {
        ...baseProps.component,
        options: undefined,
        tree: [
          {
            id: 'ou-root',
            handle: 'root-org',
            name: 'Root Org',
            children: [{id: 'ou-child', handle: 'child-org', name: 'Child Org'}],
          },
          {id: 'ou-other', handle: 'other-org', name: 'Other Org'},
        ],
      },
    };

    it('renders root-level tree nodes by name, not the flat dropdown', () => {
      const {container} = renderWithProviders(<OUSelectAdapter {...treeProps} />);
      expect(within(container).getByText('Root Org')).toBeTruthy();
      expect(within(container).getByText('Other Org')).toBeTruthy();
      expect(within(container).queryByRole('combobox')).toBeNull();
    });

    it('shows each node handle alongside its name', () => {
      const {container} = renderWithProviders(<OUSelectAdapter {...treeProps} />);
      expect(within(container).getByText('root-org')).toBeTruthy();
      expect(within(container).getByText('other-org')).toBeTruthy();
    });

    it('bounds the tree to a fixed, scrollable height so the page cannot stretch with many OUs', () => {
      const {container} = renderWithProviders(<OUSelectAdapter {...treeProps} />);
      const scrollContainer = container.querySelector('[class*="Flow--ou-tree-scroll"]');
      expect(scrollContainer).toBeTruthy();
      expect(scrollContainer).toHaveStyle({overflow: 'auto'});
    });

    it('does not submit a selection while the form is loading', async () => {
      const onInputChange = vi.fn();
      const {container} = renderWithProviders(
        <OUSelectAdapter {...treeProps} onInputChange={onInputChange} isLoading />,
      );

      await userEvent.click(within(container).getByText('Other Org'));

      expect(onInputChange).not.toHaveBeenCalled();
    });

    it('submits the clicked node id, not its handle', async () => {
      const onInputChange = vi.fn();
      const {container} = renderWithProviders(<OUSelectAdapter {...treeProps} onInputChange={onInputChange} />);

      await userEvent.click(within(container).getByText('Other Org'));

      expect(onInputChange).toHaveBeenCalledWith('ouId', 'ou-other');
    });

    it('expands to reveal nested children and submits the child id when clicked', async () => {
      const onInputChange = vi.fn();
      const {container} = renderWithProviders(<OUSelectAdapter {...treeProps} onInputChange={onInputChange} />);

      expect(within(container).queryByText('Child Org')).toBeNull();

      const expandIcons = container.querySelectorAll('.MuiTreeItem-iconContainer');
      await userEvent.click(expandIcons[0]);

      const childLabel = await within(container).findByText('Child Org');
      await userEvent.click(childLabel);

      expect(onInputChange).toHaveBeenCalledWith('ouId', 'ou-child');
    });
  });
});
