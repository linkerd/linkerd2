import _ from 'lodash';
import React from 'react';
import { Col, Row } from 'antd';

const defaultErrorMsg = "An error has occurred";

export default class ErrorMessage extends React.Component {
  constructor(props) {
    super(props);
    this.hideMessage = this.hideMessage.bind(this);
    this.state = {
      visible: true
    };
  }

  componentWillReceiveProps(newProps) {
    if (!_.isEmpty(newProps.message)) {
      this.setState({ visible :true });
    }
  }

  hideMessage() {
    this.setState({
      visible: false
    });
  }

  render() {
    return !this.state.visible ? null : (
      <Row gutter={0}>
        <div className="error-message-container">
          <Col span={20}>
            {this.props.message || defaultErrorMsg}
          </Col>
          <Col span={4}>
            <div className="dismiss" onClick={this.hideMessage} role="presentation">Dismiss X</div>
          </Col>
        </div>
      </Row>
    );
  }
}
