import _merge from 'lodash/merge';
import ApiHelpers from './util/ApiHelpers.jsx';
import TopRoutesTable from './TopRoutesTable.jsx';
import { routerWrap } from '../../test/testHelpers.jsx';
import { mount } from 'enzyme';
import '../i18n.js';

describe("Tests for <TopRoutesTable>", () => {
  const defaultProps = {
    api: ApiHelpers(""),
  };

  it("renders the table with all columns", () => {
    let extraProps = _merge({}, defaultProps, {
      rows: [{
        route: "[DEFAULT]",
        latency: {
          P50: 133,
          P95: 291,
          P99: 188
        },
        authority: "webapp",
        name: "authors:7001"
      }],
    });
    const component = mount(routerWrap(TopRoutesTable, extraProps));

    const table = component.find("BaseTable");
    expect(table).toBeDefined();
    expect(table.html()).toContain("[DEFAULT]");
    expect(table.props().tableRows).toHaveLength(1);
    expect(table.props().tableColumns).toHaveLength(7);
  });

  it("if enableFilter is true, user can filter rows by search term", () => {
    let extraProps = _merge({}, defaultProps, {
      rows: [{
        route: "[DEFAULT]",
        latency: {
          P50: 133,
          P95: 291,
          P99: 188
        },
        authority: "authors:7001"
      },{
        route: "[DEFAULT]",
        latency: {
          P50: 500,
          P95: 1,
          P99: 2
        },
        authority: "localhost:6443"
      }],
      enableFilter: true
    });
    const component = mount(routerWrap(TopRoutesTable, extraProps));

    const table = component.find("BaseTable");

    const enableFilter = table.prop("enableFilter");

    const filterIcon = table.find("FilterListIcon");

    expect(enableFilter).toEqual(true);
    expect(filterIcon).toBeDefined();
    expect(table.html()).toContain("authors:7001");
    expect(table.html()).toContain("localhost");
    filterIcon.simulate("click");
    setTimeout(() => {
      const input = table.find("input");
      input.simulate("change", {target: {value: "localhost"}});
      expect(table.html()).not.toContain("authors:7001");
      expect(table.html()).toContain("localhost");
    }, 100);
  });
});
