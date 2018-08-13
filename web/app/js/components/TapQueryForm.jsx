import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import {
  AutoComplete,
  Button,
  Col,
  Form,
  Icon,
  Input,
  Row,
  Select
} from 'antd';
import { defaultMaxRps, httpMethods, tapQueryPropType } from './util/TapUtils.js';

const colSpan = 5;
const rowGutter = 16;

const getResourceList = (resourcesByNs, ns) => {
  return resourcesByNs[ns] || _.uniq(_.flatten(_.values(resourcesByNs)));
};
export default class TapQueryForm extends React.Component {
  static propTypes = {
    awaitingWebSocketConnection: PropTypes.bool.isRequired,
    handleTapStart: PropTypes.func.isRequired,
    handleTapStop: PropTypes.func.isRequired,
    query: tapQueryPropType.isRequired,
    tapRequestInProgress: PropTypes.bool.isRequired
  }

  constructor(props) {
    super(props);

    this.state = {
      authoritiesByNs: {},
      resourcesByNs: {},
      showAdvancedForm: false,
      query: props.query,
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
      if (!_.isNil(scopeResource)) {
        // scope the available typeahead resources to the selected namespace
        state.autocomplete[scopeResource] = this.state.resourcesByNs[formVal];
      }
      if (shouldScopeAuthority) {
        state.autocomplete.authority = this.state.authoritiesByNs[formVal];
      }

      this.setState(state);
    };
  }

  handleFormEvent = name => {
    let state = {
      query: this.state.query
    };

    return event => {
      state.query[name] = event.target.value;
      this.setState(state);
    };
  }

  autoCompleteData = name => {
    return _(this.state.autocomplete[name])
      .filter(d => d.indexOf(this.state.query[name]) !== -1)
      .sortBy()
      .value();
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
                onChange={this.handleFormChange("toNamespace", "toResource")}>
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
              <AutoComplete
                dataSource={this.autoCompleteData("toResource")}
                onSelect={this.handleFormChange("toResource")}
                onSearch={this.handleFormChange("toResource")}
                placeholder="To Resource" />
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
                placeholder="Authority" />
            </Form.Item>
          </Col>

          <Col span={2 * colSpan}>
            <Form.Item
              extra="Display requests with paths that start with this prefix">
              <Input placeholder="Path" onChange={this.handleFormEvent("path")} />
            </Form.Item>
          </Col>

        </Row>

        <Row gutter={rowGutter}>
          <Col span={colSpan}>
            <Form.Item
              extra="Display requests with this scheme">
              <Input placeholder="Scheme" onChange={this.handleFormEvent("scheme")} />
            </Form.Item>
          </Col>

          <Col span={colSpan}>
            <Form.Item
              extra="Maximum requests per second to tap">
              <Input
                defaultValue={defaultMaxRps}
                placeholder="Max RPS"
                onChange={this.handleFormEvent("maxRps")} />
            </Form.Item>
          </Col>

          <Col span={colSpan}>
            <Form.Item
              extra="Display requests with this HTTP method">
              <Select
                allowClear
                placeholder="HTTP method"
                onChange={this.handleFormChange("method")}>
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
                onChange={this.handleFormChange("namespace", "resource", true)}>
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
              <AutoComplete
                dataSource={this.autoCompleteData("resource")}
                onSelect={this.handleFormChange("resource")}
                onSearch={this.handleFormChange("resource")}
                placeholder="Resource" />
            </Form.Item>
          </Col>

          <Col span={colSpan}>
            <Form.Item>
              {
              this.props.tapRequestInProgress ?
                <Button type="primary" className="tap-stop" onClick={this.props.handleTapStop}>Stop</Button> :
                <Button type="primary" className="tap-start" onClick={this.props.handleTapStart}>Start</Button>
            }
              {
              this.props.awaitingWebSocketConnection ?
                <Icon type="loading" style={{ paddingLeft: rowGutter, fontSize: 20, color: '#08c' }} /> : null
            }
            </Form.Item>
          </Col>
        </Row>

        <Button
          className="tap-form-toggle"
          onClick={() => this.toggleAdvancedForm(!this.state.showAdvancedForm)}>
          { this.state.showAdvancedForm ?
          "Hide filters" : "Show more request filters" } <Icon type={this.state.showAdvancedForm ? 'up' : 'down'} />
        </Button>

        { !this.state.showAdvancedForm ? null : this.renderAdvancedTapForm() }
      </Form>
    );
  }
}
