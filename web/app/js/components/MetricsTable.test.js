import Adapter from 'enzyme-adapter-react-16';
import ApiHelpers from './util/ApiHelpers.jsx';
import BaseTable from './BaseTable.jsx';
import { expect } from 'chai';
import { MetricsTableBase } from './MetricsTable.jsx';
import React from 'react';
import Enzyme, { shallow } from 'enzyme';

Enzyme.configure({ adapter: new Adapter() });

describe('Tests for <MetricsTableBase>', () => {
  const defaultProps = {
    api: ApiHelpers(''),
  };

  it('renders the table with all columns', () => {
    const component = shallow(
      <MetricsTableBase
        {...defaultProps}
        metrics={[{
          name: 'web',
          namespace: 'default',
          totalRequests: 0,
        }]}
        resource="deployment" />
    );

    const table = component.find(BaseTable);

    expect(table).to.have.length(1);
    expect(table.props().dataSource).to.have.length(1);
    expect(table.props().columns).to.have.length(10);
  });

  it('omits the namespace column for the namespace resource', () => {
    const component = shallow(
      <MetricsTableBase
        {...defaultProps}
        metrics={[]}
        resource="namespace" />
    );

    const table = component.find(BaseTable);

    expect(table).to.have.length(1);
    expect(table.props().columns).to.have.length(9);
  });

  it('omits the namespace column when showNamespaceColumn is false', () => {
    const component = shallow(
      <MetricsTableBase
        {...defaultProps}
        metrics={[]}
        resource="deployment"
        showNamespaceColumn={false} />
    );

    const table = component.find(BaseTable);

    expect(table).to.have.length(1);
    expect(table.props().columns).to.have.length(9);
  });

  it('omits meshed column and grafana column for authority resource', () => {
    const component = shallow(
      <MetricsTableBase
        {...defaultProps}
        metrics={[]}
        resource="authority" />
    );

    const table = component.find(BaseTable);

    expect(table).to.have.length(1);
    expect(table.props().columns).to.have.length(8);
  });

});
