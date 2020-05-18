import _merge from 'lodash/merge';
import ApiHelpers from './util/ApiHelpers.jsx';
import MetricsTable from './MetricsTable.jsx';
import { routerWrap } from '../../test/testHelpers.jsx';
import { mount } from 'enzyme';

describe('Tests for <MetricsTable>', () => {
  const defaultProps = {
    api: ApiHelpers(''),
    selectedNamespace: "default",
  };

  it('renders the table with all columns', () => {
    let extraProps = _merge({}, defaultProps, {
      metrics: [{
        name: 'web',
        namespace: 'default',
        key: 'web-default-deploy',
        totalRequests: 0,
      }],
      resource: "deployment"
    });
    const component = mount(routerWrap(MetricsTable, extraProps));

    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableRows).toHaveLength(1);
    expect(table.props().tableColumns).toHaveLength(7);
  });

  it('if enableFilter is true, user can filter rows by search term', () => {
    let extraProps = _merge({}, defaultProps, {
      metrics: [{
        name: 'authors',
        namespace: 'default',
        key: 'authors-default-deploy',
        totalRequests: 0,
      }, {
        name: 'books',
        namespace: 'default',
        key: 'books-default-deploy',
        totalRequests: 0,
      }],
      resource: 'deployment',
      enableFilter: true
    });
    const component = mount(routerWrap(MetricsTable, extraProps));

    const table = component.find('BaseTable');

    const enableFilter = table.prop('enableFilter');

    expect(enableFilter).toEqual(true);
    expect(table.html()).toContain('books');
    expect(table.html()).toContain('authors');
    table.instance().setState({ showFilter: true, filterBy: /authors/ });
    component.update();
    expect(table.html()).not.toContain('books');
    expect(table.html()).toContain('authors');
  });

  it('omits the namespace column for the namespace resource', () => {
    let extraProps = _merge({}, defaultProps, { metrics: [], resource: "namespace"});
    const component = mount(routerWrap(MetricsTable, extraProps));

    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableColumns).toHaveLength(7);
  });

  it('omits the namespace column when showNamespaceColumn is false', () => {
    let extraProps = _merge({}, defaultProps, {
      metrics: [],
      resource: "deployment",
      showNamespaceColumn: false
    });
    const component = mount(routerWrap(MetricsTable, extraProps));

    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableColumns).toHaveLength(7);
  });

  it('render table columns including jaeger', () => {
    let extraProps = _merge({}, defaultProps, {
      metrics: [],
      resource: "deployment",
      showNamespaceColumn: false,
      jaeger: 'jaeger.xyz'
    });
    const component = mount(routerWrap(MetricsTable, extraProps));
    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableColumns).toHaveLength(8);
  });

  it('render table columns including grafana', () => {
    let extraProps = _merge({}, defaultProps, {
      metrics: [],
      resource: "deployment",
      showNamespaceColumn: false,
      grafana: 'grafana.xyz'
    });
    const component = mount(routerWrap(MetricsTable, extraProps));
    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableColumns).toHaveLength(8);
  });

  it('render all table columns', () => {
    let extraProps = _merge({}, defaultProps, {
      metrics: [],
      resource: "deployment",
      showNamespaceColumn: false,
      jaeger: 'jaeger.xyz',
      grafana: 'grafana.xyz'
    });
    const component = mount(routerWrap(MetricsTable, extraProps));
    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableColumns).toHaveLength(9);
  });

  it('adds apex, leaf and weight columns, and omits meshed and grafana column, for a trafficsplit resource', () => {
    let extraProps = _merge({}, defaultProps, { metrics: [], resource: "trafficsplit"});
    const component = mount(routerWrap(MetricsTable, extraProps));

    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableColumns).toHaveLength(9);
  });
});
