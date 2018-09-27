import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import { addUrlProps, UrlQueryParamTypes } from 'react-url-query';
import Button from '@material-ui/core/Button';
import {
  AutoComplete,
  Col,
  Form,
  Icon,
  Input,
  Row,
  Select
} from 'antd';
import {
  defaultMaxRps,
  emptyTapQuery,
  httpMethods,
  tapQueryProps,
  tapQueryPropType
} from './util/TapUtils.jsx';

const colSpan = 5;
const rowGutter = 16;

// you can also tap resources to tap all pods in the resource
const resourceTypes = [
  "deployment",
  "daemonset",
  "pod",
  "replicationcontroller",
  "statefulset"
];

const getResourceList = (resourcesByNs, ns) => {
  return resourcesByNs[ns] || _.uniq(_.flatten(_.values(resourcesByNs)));
};

const urlPropsQueryConfig = _.mapValues(tapQueryProps, () => {
  return { type: UrlQueryParamTypes.string };
});

class TapQueryForm extends React.Component {
  static propTypes = {
    enableAdvancedForm: PropTypes.bool,
    handleTapClear: PropTypes.func,
    handleTapStart: PropTypes.func.isRequired,
    handleTapStop: PropTypes.func.isRequired,
    query: tapQueryPropType.isRequired,
    tapIsClosing: PropTypes.bool,
    tapRequestInProgress: PropTypes.bool.isRequired,
    updateQuery: PropTypes.func.isRequired
  }

  static defaultProps = {
    enableAdvancedForm: true,
    handleTapClear: _.noop,
    tapIsClosing: false
  }

  constructor(props) {
    super(props);

    let query = _.merge({}, props.query, _.pick(this.props, _.keys(tapQueryProps)));
    props.updateQuery(query);

    let showAdvancedForm = _.some(
      _.omit(query, ['namespace', 'resource']),
      v => !_.isEmpty(v));

    this.state = {
      query,
      showAdvancedForm,
      authoritiesByNs: {},
      resourcesByNs: {},
      autocomplete: {
        namespace: [],
        resource: [],
        toNamespace: [],
        toResource: [],
        authority: []
      },
    };
  }

  static getDerivedStateFromProps(props, state) {
    if (!_.isEqual(props.resourcesByNs, state.resourcesByNs)) {
      let resourcesByNs = props.resourcesByNs;
      let authoritiesByNs = props.authoritiesByNs;
      let namespaces = _.sortBy(_.keys(resourcesByNs));
      let resourceNames  = getResourceList(resourcesByNs, state.query.namespace);
      let toResourceNames = getResourceList(resourcesByNs, state.query.toNamespace);
      let authorities = getResourceList(authoritiesByNs, state.query.namespace);

      return _.merge(state, {
        resourcesByNs,
        authoritiesByNs,
        autocomplete: {
          namespace: namespaces,
          resource: resourceNames,
          toNamespace: namespaces,
          toResource: toResourceNames,
          authority: authorities
        }
      });
    } else {
      return null;
    }
  }

  toggleAdvancedForm = show => {
    this.setState({
      showAdvancedForm: show
    });
  }

  handleFormChange = (name, scopeResource, shouldScopeAuthority) => {
    let state = {
      query: this.state.query,
      autocomplete: this.state.autocomplete
    };

    return formVal => {
      state.query[name] = formVal;
      this.handleUrlUpdate(name, formVal);

      if (!_.isNil(scopeResource)) {
        // scope the available typeahead resources to the selected namespace
        state.autocomplete[scopeResource] = this.state.resourcesByNs[formVal];
        if (_.isEmpty(state.query[scopeResource]) || state.query[scopeResource].indexOf("namespace") !== -1) {
          state.query[scopeResource] = `namespace/${formVal}`;
          this.handleUrlUpdate(scopeResource, `namespace/${formVal}`);
        }
      }

      if (shouldScopeAuthority) {
        state.autocomplete.authority = this.state.authoritiesByNs[formVal];
      }

      this.setState(state);
      this.props.updateQuery(state.query);
    };
  }

  // Each time state.query is updated, this method calls the equivalent
  // onChange method to reflect the update in url query params. These onChange
  // methods are automatically added to props by react-url-query.
  handleUrlUpdate = (name, formVal) => {
    this.props[`onChange${_.upperFirst(name)}`](formVal);
  }

  handleFormEvent = name => {
    let state = {
      query: this.state.query
    };

    return event => {
      state.query[name] = event.target.value;
      this.handleUrlUpdate(name, event.target.value);
      this.setState(state);
      this.props.updateQuery(state.query);
    };
  }

  autoCompleteData = name => {
    return _(this.state.autocomplete[name])
      .filter(d => d.indexOf(this.state.query[name]) !== -1)
      .uniq()
      .sortBy()
      .value();
  }

  resetTapForm = () => {
    this.setState({
      query: emptyTapQuery()
    });

    _.each(this.state.query, (_val, name) => {
      this.handleUrlUpdate(name, null);
    });

    this.props.updateQuery(emptyTapQuery(), true);
    this.props.handleTapClear();
  }

  renderAdvancedTapForm = () => {
    return (
      <React.Fragment>
        <Row gutter={rowGutter}>
          <Col span={colSpan}>
            <Form.Item>
              <Select
                showSearch
                allowClear
                placeholder="To Namespace"
                optionFilterProp="children"
                onChange={this.handleFormChange("toNamespace", "toResource")}
                value={this.state.query.toNamespace}>
                {
                  _.map(this.state.autocomplete.toNamespace, (n, i) => (
                    <Select.Option key={`ns-dr-${i}`} value={n}>{n}</Select.Option>
                  ))
                }
              </Select>
            </Form.Item>
          </Col>

          <Col span={colSpan}>
            <Form.Item>
              {this.renderResourceSelect("toResource", "toNamespace")}
            </Form.Item>
          </Col>
        </Row>

        <Row gutter={rowGutter}>
          <Col span={2 * colSpan}>
            <Form.Item
              extra="Display requests with this :authority">

              <AutoComplete
                dataSource={this.autoCompleteData("authority")}
                onSelect={this.handleFormChange("authority")}
                onSearch={this.handleFormChange("authority")}
                placeholder="Authority"
                value={this.state.query.authority} />
            </Form.Item>
          </Col>

          <Col span={2 * colSpan}>
            <Form.Item
              extra="Display requests with paths that start with this prefix">
              <Input
                placeholder="Path"
                onChange={this.handleFormEvent("path")}
                value={this.state.query.path} />
            </Form.Item>
          </Col>

        </Row>

        <Row gutter={rowGutter}>
          <Col span={colSpan}>
            <Form.Item
              extra="Display requests with this scheme">
              <Input
                placeholder="Scheme"
                onChange={this.handleFormEvent("scheme")}
                value={this.state.query.scheme} />
            </Form.Item>
          </Col>

          <Col span={colSpan}>
            <Form.Item
              extra={`Maximum requests per second to tap (default ${defaultMaxRps})`}>
              <Input
                placeholder="Max RPS"
                onChange={this.handleFormEvent("maxRps")}
                value={this.state.query.maxRps} />
            </Form.Item>
          </Col>

          <Col span={colSpan}>
            <Form.Item
              extra="Display requests with this HTTP method">
              <Select
                allowClear
                placeholder="HTTP method"
                onChange={this.handleFormChange("method")}
                value={this.state.query.method}>
                {
                  _.map(httpMethods, m =>
                    <Select.Option key={`method-select-${m}`} value={m}>{m}</Select.Option>
                  )
                }
              </Select>
            </Form.Item>
          </Col>
        </Row>
      </React.Fragment>
    );
  }

  renderResourceSelect = (resourceKey, namespaceKey) => {
    let selectedNs = this.state.query[namespaceKey];
    let nsEmpty = _.isNil(selectedNs) || _.isEmpty(selectedNs);

    let resourceOptions = _.concat(
      resourceTypes,
      this.state.autocomplete[resourceKey] || [],
      nsEmpty ? [] : [`namespace/${selectedNs}`]
    );

    return (
      <Select
        showSearch
        allowClear
        disabled={nsEmpty}
        value={nsEmpty ? _.startCase(resourceKey) : this.state.query[resourceKey]}
        placeholder={_.startCase(resourceKey)}
        optionFilterProp="children"
        onChange={this.handleFormChange(resourceKey)}>
        {
        _.map(_.sortBy(resourceOptions), resource => (
          <Select.Option
            key={`${resourceKey}-${resource}`}
            value={resource}>{resource}
          </Select.Option>
          )
        )
      }
      </Select>
    );
  }

  renderTapButton = (tapInProgress, tapIsClosing) => {
    if (tapIsClosing) {
      return (<Button variant="outlined" color="primary" className="tap-ctrl tap-stop" disabled={true}>Stop</Button>);
    } else if (tapInProgress) {
      return (<Button variant="outlined" color="primary" className="tap-ctrl tap-stop" onClick={this.props.handleTapStop}>Stop</Button>);
    } else {
      return (
        <Button
          color="primary"
          variant="outlined"
          className="tap-ctrl tap-start"
          disabled={!this.state.query.namespace || !this.state.query.resource}
          onClick={this.props.handleTapStart}>
          Start
        </Button>);
    }
  }

  render() {
    return (
      <Form className="tap-form">
        <Row gutter={rowGutter}>
          <Col span={colSpan}>
            <Form.Item>
              <Select
                showSearch
                allowClear
                placeholder="Namespace"
                optionFilterProp="children"
                onChange={this.handleFormChange("namespace", "resource", true)}
                value={this.state.query.namespace}>
                {
                _.map(this.state.autocomplete.namespace, (n, i) => (
                  <Select.Option key={`ns-dr-${i}`} value={n}>{n}</Select.Option>
                ))
              }
              </Select>
            </Form.Item>
          </Col>

          <Col span={colSpan}>
            <Form.Item>
              {this.renderResourceSelect("resource", "namespace")}
            </Form.Item>
          </Col>

          <Col span={colSpan}>
            <Form.Item>
              { this.renderTapButton(this.props.tapRequestInProgress, this.props.tapIsClosing) }
              <Button onClick={this.resetTapForm} disabled={this.props.tapRequestInProgress}>Reset</Button>
            </Form.Item>
          </Col>
        </Row>

        {
          !this.props.enableAdvancedForm ? null :
          <React.Fragment>
            <Button
              className="tap-form-toggle"
              onClick={() => this.toggleAdvancedForm(!this.state.showAdvancedForm)}>
              {
                this.state.showAdvancedForm ? "Hide filters" : "Show more request filters"
              } <Icon type={this.state.showAdvancedForm ? 'up' : 'down'} />
            </Button>

            { !this.state.showAdvancedForm ? null : this.renderAdvancedTapForm() }
          </React.Fragment>
        }
      </Form>
    );
  }
}

export default addUrlProps({ urlPropsQueryConfig })(TapQueryForm);
